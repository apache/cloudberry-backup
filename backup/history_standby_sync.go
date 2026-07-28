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

package backup

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/apache/cloudberry-go-libs/operating"
	_ "github.com/mattn/go-sqlite3"
	"github.com/nightlyone/lockfile"
)

const (
	backupHistoryDBName                    = "gpbackup_history.db"
	backupHistoryStandbySyncSSHOptions     = "ssh -o StrictHostKeyChecking=no -o ConnectTimeout=30"
	backupHistoryStandbySyncTempDirPattern = "gpbackup-history-standby-sync-*"
	backupHistoryStandbySyncStandbySQL     = "SELECT hostname, datadir FROM gp_segment_configuration WHERE content = -1 AND role = 'm' AND status = 'u';"
)

type backupHistoryStandbySyncTarget struct {
	sourceDBPath         string
	standbyHost          string
	standbyDataDir       string
	standbyHistoryDBPath string
}

type backupHistoryStandbySyncStandby struct {
	Hostname string `db:"hostname"`
	DataDir  string `db:"datadir"`
}

type backupHistoryStandbySyncCommand interface {
	CombinedOutput() ([]byte, error)
}

var (
	backupHistoryStandbySync = syncBackupHistoryToStandby

	backupHistoryStandbySyncCommandExec = func(name string, args ...string) backupHistoryStandbySyncCommand {
		return exec.Command(name, args...)
	}
	backupHistoryStandbySyncMkdirTemp   = os.MkdirTemp
	backupHistoryStandbySyncRemoveAll   = os.RemoveAll
	backupHistoryStandbySyncCurrentUser = func() (string, error) {
		currentUser, err := operating.System.CurrentUser()
		if err != nil {
			return "", err
		}
		return currentUser.Username, nil
	}
)

func syncBackupHistoryToStandbyBestEffort(disabled bool) (string, error) {
	if disabled {
		skipReason := "disabled by --" + options.NO_HISTORY_SYNC_STANDBY
		gplog.Info("Skipping history db sync to standby coordinator: %s", skipReason)
		return skipReason, nil
	}

	skipReason, err := backupHistoryStandbySync()
	if err != nil {
		gplog.Warn("History db sync to standby coordinator failed; standby history may be stale: %v", err)
		return "", err
	}
	if skipReason != "" {
		gplog.Debug("Skipping history db sync to standby coordinator: %s", skipReason)
	}
	return skipReason, nil
}

func syncBackupHistoryToStandbyAfterCleanup(backupFailed bool, historyUpdated bool) {
	if backupFailed || !historyUpdated || MustGetFlagBool(options.NO_HISTORY) {
		return
	}
	_, _ = syncBackupHistoryToStandbyBestEffort(MustGetFlagBool(options.NO_HISTORY_SYNC_STANDBY))
}

func syncBackupHistoryToStandby() (string, error) {
	sourceDBPath, sourceInfo, err := canonicalBackupHistoryStandbySyncSource(globalFPInfo.GetBackupHistoryDatabasePath())
	if err != nil {
		return "", err
	}

	target, skipReason, err := discoverBackupHistoryStandbySyncTarget(sourceDBPath)
	if err != nil {
		return "", err
	}
	if skipReason != "" {
		return skipReason, nil
	}

	userName, err := backupHistoryStandbySyncCurrentUser()
	if err != nil {
		return "", fmt.Errorf("resolve current OS user for standby history sync: %w", err)
	}

	err = withBackupHistoryStandbySyncLock(sourceDBPath, func() error {
		return withBackupHistoryStandbySyncSnapshot(sourceDBPath, sourceInfo.Mode().Perm(), func(snapshotPath string) error {
			return syncBackupHistoryStandbySnapshot(target, userName, snapshotPath)
		})
	})
	if err != nil {
		return "", err
	}
	return "", nil
}

func canonicalBackupHistoryStandbySyncSource(sourceDBPath string) (string, os.FileInfo, error) {
	absoluteSourceDBPath, err := filepath.Abs(filepath.Clean(sourceDBPath))
	if err != nil {
		return "", nil, fmt.Errorf("resolve absolute source history db path for standby sync: %w", err)
	}
	canonicalSourceDBPath, err := filepath.EvalSymlinks(absoluteSourceDBPath)
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

func discoverBackupHistoryStandbySyncTarget(sourceDBPath string) (*backupHistoryStandbySyncTarget, string, error) {
	standby, err := queryBackupHistoryStandbySyncStandby()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "no up standby coordinator found", nil
		}
		return nil, "", fmt.Errorf("query up standby coordinator for standby history sync discovery: %w", err)
	}
	target := &backupHistoryStandbySyncTarget{
		sourceDBPath:         sourceDBPath,
		standbyHost:          standby.Hostname,
		standbyDataDir:       standby.DataDir,
		standbyHistoryDBPath: filepath.Join(standby.DataDir, backupHistoryDBName),
	}
	gplog.Debug("Discovered standby history sync target: source=%s standby=%s:%s", target.sourceDBPath, target.standbyHost, target.standbyHistoryDBPath)
	return target, "", nil
}

func queryBackupHistoryStandbySyncStandby() (backupHistoryStandbySyncStandby, error) {
	var standby backupHistoryStandbySyncStandby
	if connectionPool == nil {
		return standby, errors.New("connection pool is not initialized")
	}
	err := connectionPool.Get(&standby, backupHistoryStandbySyncStandbySQL)
	return standby, err
}

func withBackupHistoryStandbySyncLock(sourceDBPath string, syncFn func() error) error {
	lockPath := backupHistoryStandbySyncLockPath(sourceDBPath)
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

func backupHistoryStandbySyncLockPath(sourceDBPath string) string {
	return sourceDBPath + ".sync.lock"
}

func withBackupHistoryStandbySyncSnapshot(sourceDBPath string, sourceMode os.FileMode, syncFn func(string) error) error {
	snapshotPath, tempDir, err := createBackupHistoryStandbySyncSnapshot(sourceDBPath, sourceMode)
	if tempDir != "" {
		defer cleanupBackupHistoryStandbySyncTempDir(tempDir)
	}
	if err != nil {
		return err
	}
	return syncFn(snapshotPath)
}

func createBackupHistoryStandbySyncSnapshot(sourceDBPath string, sourceMode os.FileMode) (string, string, error) {
	tempDir, err := backupHistoryStandbySyncMkdirTemp("", backupHistoryStandbySyncTempDirPattern)
	if err != nil {
		return "", "", fmt.Errorf("create local standby history sync temp directory: %w", err)
	}
	snapshotPath := filepath.Join(tempDir, backupHistoryDBName)
	if err := vacuumBackupHistoryStandbySyncSnapshot(sourceDBPath, snapshotPath); err != nil {
		cleanupBackupHistoryStandbySyncTempDir(tempDir)
		return "", "", err
	}
	if err := os.Chmod(snapshotPath, sourceMode); err != nil {
		cleanupBackupHistoryStandbySyncTempDir(tempDir)
		return "", "", fmt.Errorf("set standby history sync snapshot permissions from source history db: %w", err)
	}
	if err := validateBackupHistoryStandbySyncSnapshot(snapshotPath); err != nil {
		cleanupBackupHistoryStandbySyncTempDir(tempDir)
		return "", "", err
	}
	return snapshotPath, tempDir, nil
}

func vacuumBackupHistoryStandbySyncSnapshot(sourceDBPath, snapshotPath string) error {
	sourceDB, err := sql.Open("sqlite3", backupHistoryStandbySyncSQLiteURI(sourceDBPath, "ro"))
	if err != nil {
		return fmt.Errorf("open source history db for standby sync snapshot: %w", err)
	}
	defer sourceDB.Close()

	if _, err := sourceDB.Exec("VACUUM main INTO ?", snapshotPath); err != nil {
		return fmt.Errorf("create standby history sync snapshot with VACUUM INTO: %w", err)
	}
	return nil
}

func validateBackupHistoryStandbySyncSnapshot(snapshotPath string) error {
	results, err := runBackupHistoryStandbySyncQuickCheck(snapshotPath)
	if err != nil {
		return err
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("validate standby history sync snapshot quick_check: expected single ok result, got %v", results)
	}
	return nil
}

func runBackupHistoryStandbySyncQuickCheck(snapshotPath string) ([]string, error) {
	snapshotDB, err := sql.Open("sqlite3", backupHistoryStandbySyncSQLiteURI(snapshotPath, "ro"))
	if err != nil {
		return nil, fmt.Errorf("open standby history sync snapshot read-only: %w", err)
	}
	defer snapshotDB.Close()

	rows, err := snapshotDB.Query("PRAGMA quick_check")
	if err != nil {
		return nil, fmt.Errorf("run PRAGMA quick_check on standby history sync snapshot: %w", err)
	}
	defer rows.Close()

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

func cleanupBackupHistoryStandbySyncTempDir(tempDir string) {
	if err := backupHistoryStandbySyncRemoveAll(tempDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		gplog.Debug("Unable to remove local standby history sync temp directory %s: %v", tempDir, err)
	}
}

func syncBackupHistoryStandbySnapshot(target *backupHistoryStandbySyncTarget, userName, snapshotPath string) error {
	remoteTempPath := newBackupHistoryStandbySyncRemoteTempPath(target.standbyDataDir, snapshotPath)
	if err := rsyncBackupHistoryStandbySyncSnapshot(snapshotPath, target.standbyHost, userName, remoteTempPath); err != nil {
		return cleanupBackupHistoryStandbySyncRemoteTempAfterError(err, target.standbyHost, userName, remoteTempPath)
	}
	if err := installBackupHistoryStandbySyncSnapshot(target, userName, remoteTempPath); err != nil {
		return cleanupBackupHistoryStandbySyncRemoteTempAfterError(err, target.standbyHost, userName, remoteTempPath)
	}
	return nil
}

func newBackupHistoryStandbySyncRemoteTempPath(standbyDataDir, snapshotPath string) string {
	return filepath.Join(standbyDataDir, fmt.Sprintf(".%s.%s.tmp", backupHistoryDBName, filepath.Base(filepath.Dir(snapshotPath))))
}

func rsyncBackupHistoryStandbySyncSnapshot(snapshotPath, standbyHost, userName, remoteTempPath string) error {
	args := buildBackupHistoryStandbySyncRsyncArgs(snapshotPath, standbyHost, userName, remoteTempPath)
	gplog.Debug("Transfer history db snapshot to standby coordinator: %s -> %s:%s", snapshotPath, standbyHost, remoteTempPath)
	output, err := backupHistoryStandbySyncCommandExec("rsync", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync standby history snapshot to %s:%s failed: %w%s", standbyHost, remoteTempPath, err, formatBackupHistoryStandbySyncCommandOutput(output))
	}
	return nil
}

func buildBackupHistoryStandbySyncRsyncArgs(snapshotPath, standbyHost, userName, remoteTempPath string) []string {
	return []string{
		"-p",
		"-e",
		backupHistoryStandbySyncSSHOptions,
		"--",
		snapshotPath,
		fmt.Sprintf("%s@%s:%s", userName, standbyHost, shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)),
	}
}

func installBackupHistoryStandbySyncSnapshot(target *backupHistoryStandbySyncTarget, userName, remoteTempPath string) error {
	command := buildBackupHistoryStandbySyncRemoteInstallCommand(remoteTempPath, target.standbyHistoryDBPath)
	gplog.Debug("Install history db snapshot on standby coordinator: %s:%s", target.standbyHost, target.standbyHistoryDBPath)
	output, err := runBackupHistoryStandbySyncSSHCommand(command, target.standbyHost, userName)
	if err != nil {
		return fmt.Errorf("install standby history snapshot on %s:%s failed: %w%s", target.standbyHost, target.standbyHistoryDBPath, err, formatBackupHistoryStandbySyncCommandOutput(output))
	}
	return nil
}

func buildBackupHistoryStandbySyncRemoteInstallCommand(remoteTempPath, standbyHistoryDBPath string) string {
	quotedTempPath := shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)
	quotedHistoryDBPath := shellQuoteBackupHistoryStandbySyncPath(standbyHistoryDBPath)
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

func cleanupBackupHistoryStandbySyncRemoteTempAfterError(primaryErr error, standbyHost, userName, remoteTempPath string) error {
	if cleanupErr := cleanupBackupHistoryStandbySyncRemoteTemp(standbyHost, userName, remoteTempPath); cleanupErr != nil {
		return fmt.Errorf("%w; additionally failed to clean up remote temp file: %v", primaryErr, cleanupErr)
	}
	return primaryErr
}

func cleanupBackupHistoryStandbySyncRemoteTemp(standbyHost, userName, remoteTempPath string) error {
	command := buildBackupHistoryStandbySyncRemoteCleanupCommand(remoteTempPath)
	gplog.Debug("Clean up remote standby history sync temp file: %s:%s", standbyHost, remoteTempPath)
	output, err := runBackupHistoryStandbySyncSSHCommand(command, standbyHost, userName)
	if err != nil {
		return fmt.Errorf("clean up remote standby history sync temp file %s:%s failed: %w%s", standbyHost, remoteTempPath, err, formatBackupHistoryStandbySyncCommandOutput(output))
	}
	return nil
}

func buildBackupHistoryStandbySyncRemoteCleanupCommand(remoteTempPath string) string {
	return fmt.Sprintf("rm -f -- %s", shellQuoteBackupHistoryStandbySyncPath(remoteTempPath))
}

func runBackupHistoryStandbySyncSSHCommand(remoteCommand, standbyHost, userName string) ([]byte, error) {
	return backupHistoryStandbySyncCommandExec(
		"ssh",
		"-o",
		"StrictHostKeyChecking=no",
		"-o",
		"ConnectTimeout=30",
		fmt.Sprintf("%s@%s", userName, standbyHost),
		remoteCommand,
	).CombinedOutput()
}

func backupHistoryStandbySyncSQLiteURI(dbPath, mode string) string {
	query := url.Values{}
	query.Set("mode", mode)
	dbURI := url.URL{Scheme: "file", Path: dbPath, RawQuery: query.Encode()}
	return dbURI.String()
}

func shellQuoteBackupHistoryStandbySyncPath(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func formatBackupHistoryStandbySyncCommandOutput(output []byte) string {
	trimmedOutput := strings.TrimSpace(string(output))
	if trimmedOutput == "" {
		return ""
	}
	return ": " + trimmedOutput
}
