/*
Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements.  See the NOTICE file
distributed with this work for additional information
regarding copyright ownership.  The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License.  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
*/

package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/apache/cloudberry-go-libs/operating"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nightlyone/lockfile"
)

const (
	historyStandbySyncSSHOptions     = "ssh -o StrictHostKeyChecking=no -o ConnectTimeout=30"
	historyStandbySyncTempDirPattern = "gpbackman-history-standby-sync-%s-%d-*"
)

type historyStandbySyncResult struct {
	skipReason string
	err        error
}

type historyStandbySyncTarget struct {
	sourceDBPath         string
	sourceMode           os.FileMode
	standbyHost          string
	standbyDataDir       string
	standbyHistoryDBPath string
}

var (
	historyStandbySync                = syncHistoryStandby
	historyStandbySyncOpenClusterConn = gpbckpconfig.NewClusterLocalClusterDefaultConn
	historyStandbySyncMkdirTemp       = os.MkdirTemp
	historyStandbySyncRemoveAll       = os.RemoveAll
	historyStandbySyncNow             = time.Now
	historyStandbySyncPID             = os.Getpid
	historyStandbySyncCurrentUser     = func() (string, error) {
		currentUser, err := operating.System.CurrentUser()
		if err != nil {
			return "", err
		}
		return currentUser.Username, nil
	}
	historyStandbySyncRunSSHCommand = runHistoryStandbySyncSSHCommand
)

func syncHistoryStandbyBestEffort(disabled bool) historyStandbySyncResult {
	if disabled {
		result := historyStandbySyncResult{skipReason: "disabled by --" + noHistorySyncStandbyFlagName}
		gplog.Info("%s", textmsg.InfoTextHistoryStandbySyncSkip(result.skipReason))
		return result
	}

	result := historyStandbySync()
	if result.err != nil {
		gplog.Warn("%s", textmsg.WarnTextHistoryStandbySyncFailed(result.err))
		return result
	}
	if result.skipReason != "" {
		gplog.Debug("%s", textmsg.InfoTextHistoryStandbySyncSkip(result.skipReason))
	}
	return result
}

func syncHistoryStandbyStrict() error {
	result := historyStandbySync()
	if result.err != nil {
		return result.err
	}
	if result.skipReason != "" {
		return textmsg.ErrorHistoryStandbySyncSkippedError(result.skipReason)
	}
	return nil
}

func syncHistoryStandby() historyStandbySyncResult {
	sourceDBPath, skipReason := getHistoryStandbySyncSourceDBPath()
	if skipReason != "" {
		return historyStandbySyncResult{skipReason: skipReason}
	}

	target, skipReason, err := discoverHistoryStandbySyncTarget(sourceDBPath)
	if err != nil {
		return historyStandbySyncResult{err: err}
	}
	if skipReason != "" {
		return historyStandbySyncResult{skipReason: skipReason}
	}

	userName, err := historyStandbySyncCurrentUser()
	if err != nil {
		return historyStandbySyncResult{err: fmt.Errorf("resolve current OS user for standby history sync: %w", err)}
	}

	gplog.Info("%s", textmsg.InfoTextHistoryStandbySyncStart(target.sourceDBPath))
	err = withHistoryStandbySyncLock(target.sourceDBPath, func() error {
		return withHistoryStandbySyncSnapshot(target.sourceDBPath, target.sourceMode, func(snapshotPath string) error {
			return syncHistoryStandbySnapshotToStandby(target, userName, snapshotPath)
		})
	})
	if err != nil {
		return historyStandbySyncResult{err: err}
	}
	gplog.Info("%s", textmsg.InfoTextHistoryStandbySyncSuccess(target.standbyHost, target.standbyHistoryDBPath))
	return historyStandbySyncResult{}
}

func getHistoryStandbySyncSourceDBPath() (string, string) {
	sourceDBPath := getHistoryDBPath(rootHistoryDB, rootAutoLoadHistoryDB)
	if rootHistoryDB == "" && !rootAutoLoadHistoryDB {
		return sourceDBPath, "using default working-directory history db"
	}
	if rootHistoryDB == "" && rootAutoLoadHistoryDB && sourceDBPath == historyDBNameConst {
		return sourceDBPath, "--auto-load-history-db did not resolve the cluster history db"
	}
	return sourceDBPath, ""
}

func discoverHistoryStandbySyncTarget(sourceDBPath string) (*historyStandbySyncTarget, string, error) {
	db, err := historyStandbySyncOpenClusterConn()
	if err != nil {
		return nil, "", fmt.Errorf("connect to local cluster for standby history sync discovery: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	primaryDataDir, err := queryHistoryStandbySyncPrimaryDataDir(db)
	if err != nil {
		return nil, "", fmt.Errorf("query primary coordinator datadir for standby history sync discovery: %w", err)
	}

	canonicalSourceDBPath, sourceInfo, err := canonicalHistoryStandbySyncSource(sourceDBPath)
	if err != nil {
		return nil, "", err
	}
	canonicalPrimaryHistoryDBPath, err := canonicalHistoryStandbySyncPath(filepath.Join(primaryDataDir, historyDBNameConst))
	if err != nil {
		return nil, "", fmt.Errorf("resolve canonical primary history db path for standby sync: %w", err)
	}
	if canonicalSourceDBPath != canonicalPrimaryHistoryDBPath {
		return nil, fmt.Sprintf("source history db %s is not cluster history db %s", canonicalSourceDBPath, canonicalPrimaryHistoryDBPath), nil
	}

	standbyConfig, err := queryHistoryStandbySyncStandby(db)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "no up standby coordinator found", nil
		}
		return nil, "", fmt.Errorf("query up standby coordinator for standby history sync discovery: %w", err)
	}

	target := &historyStandbySyncTarget{
		sourceDBPath:         canonicalSourceDBPath,
		sourceMode:           sourceInfo.Mode().Perm(),
		standbyHost:          standbyConfig.Hostname,
		standbyDataDir:       standbyConfig.DataDir,
		standbyHistoryDBPath: filepath.Join(standbyConfig.DataDir, historyDBNameConst),
	}
	gplog.Debug("Discovered standby history sync target: source=%s standby=%s:%s", target.sourceDBPath, target.standbyHost, target.standbyHistoryDBPath)
	return target, "", nil
}

func queryHistoryStandbySyncPrimaryDataDir(db *sqlx.DB) (string, error) {
	return gpbckpconfig.QueryPrimaryCoordinatorDataDir(db)
}

func queryHistoryStandbySyncStandby(db *sqlx.DB) (gpbckpconfig.StandbyCoordinator, error) {
	return gpbckpconfig.QueryUpStandbyCoordinator(db)
}

func canonicalHistoryStandbySyncSource(sourceDBPath string) (string, os.FileInfo, error) {
	canonicalSourceDBPath, err := canonicalHistoryStandbySyncPath(sourceDBPath)
	if err != nil {
		return "", nil, fmt.Errorf("resolve canonical source history db path for standby sync: %w", err)
	}
	sourceInfo, err := os.Stat(canonicalSourceDBPath)
	if err != nil {
		return "", nil, fmt.Errorf("stat source history db for standby sync: %w", err)
	}
	if !sourceInfo.Mode().IsRegular() {
		return "", nil, fmt.Errorf("source history db for standby sync is not a regular file: %s", canonicalSourceDBPath)
	}
	return canonicalSourceDBPath, sourceInfo, nil
}

func canonicalHistoryStandbySyncPath(path string) (string, error) {
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	canonicalPath, err := filepath.EvalSymlinks(absolutePath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(canonicalPath), nil
}

func withHistoryStandbySyncLock(sourceDBPath string, syncFn func() error) error {
	lockPath := historyStandbySyncLockPath(sourceDBPath)
	sourceLock, err := lockfile.New(lockPath)
	if err != nil {
		return fmt.Errorf("create standby history sync lock %s: %w", lockPath, err)
	}
	if err := sourceLock.TryLock(); err != nil {
		return fmt.Errorf("lock standby history sync source %s: %w", sourceDBPath, err)
	}

	syncErr := syncFn()
	unlockErr := sourceLock.Unlock()
	if syncErr != nil {
		if unlockErr != nil {
			return fmt.Errorf("%w; additionally failed to release standby history sync lock %s: %v", syncErr, lockPath, unlockErr)
		}
		return syncErr
	}
	if unlockErr != nil {
		return fmt.Errorf("release standby history sync lock %s: %w", lockPath, unlockErr)
	}
	return nil
}

func historyStandbySyncLockPath(sourceDBPath string) string {
	return sourceDBPath + ".sync.lock"
}

func withHistoryStandbySyncSnapshot(sourceDBPath string, sourceMode os.FileMode, syncFn func(string) error) error {
	snapshotPath, tempDir, err := createHistoryStandbySyncSnapshot(sourceDBPath, sourceMode)
	if tempDir != "" {
		defer cleanupHistoryStandbySyncTempDir(tempDir)
	}
	if err != nil {
		return err
	}
	return syncFn(snapshotPath)
}

func createHistoryStandbySyncSnapshot(sourceDBPath string, sourceMode os.FileMode) (string, string, error) {
	tempDirPattern := fmt.Sprintf(
		historyStandbySyncTempDirPattern,
		historyStandbySyncNow().UTC().Format("20060102150405"),
		historyStandbySyncPID(),
	)
	tempDir, err := historyStandbySyncMkdirTemp("", tempDirPattern)
	if err != nil {
		return "", "", fmt.Errorf("create local standby history sync temp directory: %w", err)
	}
	snapshotPath := filepath.Join(tempDir, historyDBNameConst)
	if err := vacuumHistoryStandbySyncSnapshot(sourceDBPath, snapshotPath); err != nil {
		cleanupHistoryStandbySyncTempDir(tempDir)
		return "", "", err
	}
	if err := os.Chmod(snapshotPath, sourceMode); err != nil {
		cleanupHistoryStandbySyncTempDir(tempDir)
		return "", "", fmt.Errorf("set standby history sync snapshot permissions from source history db: %w", err)
	}
	if err := validateHistoryStandbySyncSnapshot(snapshotPath); err != nil {
		cleanupHistoryStandbySyncTempDir(tempDir)
		return "", "", err
	}
	return snapshotPath, tempDir, nil
}

func vacuumHistoryStandbySyncSnapshot(sourceDBPath, snapshotPath string) error {
	sourceDB, err := sql.Open("sqlite3", historyStandbySyncSQLiteURI(sourceDBPath, "ro"))
	if err != nil {
		return fmt.Errorf("open source history db for standby sync snapshot: %w", err)
	}
	defer func() {
		_ = sourceDB.Close()
	}()

	if _, err := sourceDB.Exec("VACUUM main INTO ?", snapshotPath); err != nil {
		return fmt.Errorf("create standby history sync snapshot with VACUUM INTO: %w", err)
	}
	return nil
}

func validateHistoryStandbySyncSnapshot(snapshotPath string) error {
	results, err := runHistoryStandbySyncQuickCheck(snapshotPath)
	if err != nil {
		return err
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("validate standby history sync snapshot quick_check: expected single ok result, got %v", results)
	}
	return nil
}

func runHistoryStandbySyncQuickCheck(snapshotPath string) ([]string, error) {
	snapshotDB, err := sql.Open("sqlite3", historyStandbySyncSQLiteURI(snapshotPath, "ro"))
	if err != nil {
		return nil, fmt.Errorf("open standby history sync snapshot read-only: %w", err)
	}
	defer func() {
		_ = snapshotDB.Close()
	}()

	rows, err := snapshotDB.Query("PRAGMA quick_check")
	if err != nil {
		return nil, fmt.Errorf("run PRAGMA quick_check on standby history sync snapshot: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	results := make([]string, 0)
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return nil, fmt.Errorf("scan PRAGMA quick_check result for standby history sync snapshot: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read PRAGMA quick_check results for standby history sync snapshot: %w", err)
	}
	return results, nil
}

func cleanupHistoryStandbySyncTempDir(tempDir string) {
	if err := historyStandbySyncRemoveAll(tempDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		gplog.Debug("Unable to remove local standby history sync temp directory %s: %v", tempDir, err)
	}
}

func syncHistoryStandbySnapshotToStandby(target *historyStandbySyncTarget, userName, snapshotPath string) error {
	remoteTempPath := newHistoryStandbySyncRemoteTempPath(target.standbyDataDir, snapshotPath)
	if err := rsyncHistoryStandbySyncSnapshot(snapshotPath, target.standbyHost, userName, remoteTempPath); err != nil {
		return cleanupHistoryStandbySyncRemoteTempAfterError(err, target.standbyHost, userName, remoteTempPath)
	}
	if err := installHistoryStandbySyncSnapshotOnStandby(target, userName, remoteTempPath); err != nil {
		return cleanupHistoryStandbySyncRemoteTempAfterError(err, target.standbyHost, userName, remoteTempPath)
	}
	return nil
}

func newHistoryStandbySyncRemoteTempPath(standbyDataDir, snapshotPath string) string {
	return filepath.Join(standbyDataDir, fmt.Sprintf(".%s.%s.tmp", historyDBNameConst, filepath.Base(filepath.Dir(snapshotPath))))
}

func rsyncHistoryStandbySyncSnapshot(snapshotPath, standbyHost, userName, remoteTempPath string) error {
	args := buildHistoryStandbySyncRsyncArgs(snapshotPath, standbyHost, userName, remoteTempPath)
	gplog.Debug("Transfer history db snapshot to standby coordinator: %s -> %s:%s", snapshotPath, standbyHost, remoteTempPath)
	output, err := execCombinedOutputCommand("rsync", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync standby history snapshot to %s:%s failed: %w%s", standbyHost, remoteTempPath, err, formatHistoryStandbySyncCommandOutput(output))
	}
	return nil
}

func buildHistoryStandbySyncRsyncArgs(snapshotPath, standbyHost, userName, remoteTempPath string) []string {
	return []string{
		"-p",
		"-e",
		historyStandbySyncSSHOptions,
		"--",
		snapshotPath,
		fmt.Sprintf("%s@%s:%s", userName, standbyHost, shellQuoteHistoryStandbySyncPath(remoteTempPath)),
	}
}

func installHistoryStandbySyncSnapshotOnStandby(target *historyStandbySyncTarget, userName, remoteTempPath string) error {
	command := buildHistoryStandbySyncRemoteInstallCommand(remoteTempPath, target.standbyHistoryDBPath)
	gplog.Debug("Install history db snapshot on standby coordinator: %s:%s", target.standbyHost, target.standbyHistoryDBPath)
	output, err := historyStandbySyncRunSSHCommand(command, target.standbyHost, userName)
	if err != nil {
		return fmt.Errorf("install standby history snapshot on %s:%s failed: %w%s", target.standbyHost, target.standbyHistoryDBPath, err, formatHistoryStandbySyncCommandOutput(output))
	}
	return nil
}

func buildHistoryStandbySyncRemoteInstallCommand(remoteTempPath, standbyHistoryDBPath string) string {
	quotedTempPath := shellQuoteHistoryStandbySyncPath(remoteTempPath)
	quotedHistoryDBPath := shellQuoteHistoryStandbySyncPath(standbyHistoryDBPath)
	return fmt.Sprintf(
		"test -f %s && if test -e %s; then chown --reference=%s -- %s && chmod --reference=%s -- %s; fi && mv -f -- %s %s",
		quotedTempPath,
		quotedHistoryDBPath,
		quotedHistoryDBPath,
		quotedTempPath,
		quotedHistoryDBPath,
		quotedTempPath,
		quotedTempPath,
		quotedHistoryDBPath,
	)
}

func cleanupHistoryStandbySyncRemoteTempAfterError(primaryErr error, standbyHost, userName, remoteTempPath string) error {
	if cleanupErr := cleanupHistoryStandbySyncRemoteTemp(standbyHost, userName, remoteTempPath); cleanupErr != nil {
		return fmt.Errorf("%w; additionally failed to clean up remote temp file: %v", primaryErr, cleanupErr)
	}
	return primaryErr
}

func cleanupHistoryStandbySyncRemoteTemp(standbyHost, userName, remoteTempPath string) error {
	command := buildHistoryStandbySyncRemoteCleanupCommand(remoteTempPath)
	gplog.Debug("Clean up remote standby history sync temp file: %s:%s", standbyHost, remoteTempPath)
	output, err := historyStandbySyncRunSSHCommand(command, standbyHost, userName)
	if err != nil {
		return fmt.Errorf("clean up remote standby history sync temp file %s:%s failed: %w%s", standbyHost, remoteTempPath, err, formatHistoryStandbySyncCommandOutput(output))
	}
	return nil
}

func buildHistoryStandbySyncRemoteCleanupCommand(remoteTempPath string) string {
	return fmt.Sprintf("rm -f -- %s", shellQuoteHistoryStandbySyncPath(remoteTempPath))
}

func runHistoryStandbySyncSSHCommand(remoteCommand, standbyHost, userName string) ([]byte, error) {
	return execCombinedOutputCommand(
		"ssh",
		"-o",
		"StrictHostKeyChecking=no",
		"-o",
		"ConnectTimeout=30",
		fmt.Sprintf("%s@%s", userName, standbyHost),
		remoteCommand,
	).CombinedOutput()
}

func historyStandbySyncSQLiteURI(dbPath, mode string) string {
	query := url.Values{}
	query.Set("mode", mode)
	dbURI := url.URL{Scheme: "file", Path: dbPath, RawQuery: query.Encode()}
	return dbURI.String()
}

func shellQuoteHistoryStandbySyncPath(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func formatHistoryStandbySyncCommandOutput(output []byte) string {
	trimmedOutput := strings.TrimSpace(string(output))
	if trimmedOutput == "" {
		return ""
	}
	return ": " + trimmedOutput
}
