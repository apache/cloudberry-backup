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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	backupfilepath "github.com/apache/cloudberry-backup/filepath"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/apache/cloudberry-go-libs/testhelper"
	"github.com/jmoiron/sqlx"
	"github.com/nightlyone/lockfile"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type backupHistoryStandbySyncCommandCall struct {
	ctx         context.Context
	ctxErr      error
	deadline    time.Time
	hasDeadline bool
	name        string
	args        []string
}

type backupHistoryStandbySyncCommandResponse struct {
	output []byte
	err    error
}

type backupHistoryStandbySyncFakeCommand struct {
	output []byte
	err    error
}

func (c backupHistoryStandbySyncFakeCommand) CombinedOutput() ([]byte, error) {
	return c.output, c.err
}

var _ = Describe("backup history standby sync", func() {
	var (
		originalSync               func() (string, error)
		originalOpenSQLite         func(string, string) (*sql.DB, error)
		originalContextWithTimeout func(context.Context, time.Duration) (context.Context, context.CancelFunc)
	)

	BeforeEach(func() {
		testhelper.SetupTestLogger()
		cmdFlags = pflag.NewFlagSet("gpbackup", pflag.ContinueOnError)
		options.SetBackupFlagDefaults(cmdFlags)
		globalFPInfo = backupfilepath.FilePathInfo{}
		connectionPool = nil
		originalSync = backupHistoryStandbySync
		originalOpenSQLite = backupHistoryStandbySyncOpenSQLite
		originalContextWithTimeout = backupHistoryStandbySyncContextWithTimeout
		backupHistoryStandbySync = syncBackupHistoryToStandby
		backupHistoryStandbySyncOpenSQLite = sql.Open
		backupHistoryStandbySyncContextWithTimeout = context.WithTimeout
		backupHistoryStandbySyncCommandExec = func(ctx context.Context, name string, args ...string) backupHistoryStandbySyncCommand {
			return backupHistoryStandbySyncFakeCommand{}
		}
		backupHistoryStandbySyncMkdirTemp = os.MkdirTemp
		backupHistoryStandbySyncRemoveAll = os.RemoveAll
		backupHistoryStandbySyncCurrentUser = func() (string, error) {
			return "gpadmin", nil
		}
	})

	AfterEach(func() {
		backupHistoryStandbySync = originalSync
		backupHistoryStandbySyncOpenSQLite = originalOpenSQLite
		backupHistoryStandbySyncContextWithTimeout = originalContextWithTimeout
		backupHistoryStandbySyncCommandExec = func(ctx context.Context, name string, args ...string) backupHistoryStandbySyncCommand {
			return backupHistoryStandbySyncFakeCommand{}
		}
		backupHistoryStandbySyncMkdirTemp = os.MkdirTemp
		backupHistoryStandbySyncRemoveAll = os.RemoveAll
		backupHistoryStandbySyncCurrentUser = func() (string, error) {
			return "gpadmin", nil
		}
		if connectionPool != nil {
			connectionPool.Close()
		}
	})

	It("creates a verified snapshot with source permissions", func() {
		tmpDir := GinkgoT().TempDir()
		sourcePath := filepath.Join(tmpDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		Expect(os.Chmod(sourcePath, 0o640)).To(Succeed())

		snapshotPath, tempDir, err := createBackupHistoryStandbySyncSnapshot(sourcePath, 0o640)
		Expect(err).ToNot(HaveOccurred())
		defer cleanupBackupHistoryStandbySyncTempDir(tempDir)

		Expect(snapshotPath).To(Equal(filepath.Join(tempDir, backupHistoryDBName)))
		snapshotInfo, err := os.Stat(snapshotPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshotInfo.Mode().Perm()).To(Equal(os.FileMode(0o640)))

		snapshotDB, err := sql.Open("sqlite3", backupHistoryStandbySyncSQLiteURI(snapshotPath, "ro"))
		Expect(err).ToNot(HaveOccurred())
		defer snapshotDB.Close()
		var value string
		Expect(snapshotDB.QueryRow("SELECT value FROM sync_test WHERE id = 1").Scan(&value)).To(Succeed())
		Expect(value).To(Equal("present"))
		Expect(validateBackupHistoryStandbySyncSnapshot(snapshotPath)).To(Succeed())
	})

	It("rejects corrupted SQLite sources before transport", func() {
		tmpDir := GinkgoT().TempDir()
		sourcePath := filepath.Join(tmpDir, backupHistoryDBName)
		Expect(os.WriteFile(sourcePath, []byte("not sqlite"), 0o600)).To(Succeed())

		_, tempDir, err := createBackupHistoryStandbySyncSnapshot(sourcePath, 0o600)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("VACUUM INTO"))
		Expect(tempDir).To(BeEmpty())
	})

	It("returns SQLite close errors without changing the gpbackup error code", func() {
		sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		closeErr := errors.New("close failed")
		mock.ExpectExec("VACUUM main INTO ?").
			WithArgs("/tmp/snapshot.db").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectClose().WillReturnError(closeErr)
		backupHistoryStandbySyncOpenSQLite = func(driverName, dataSourceName string) (*sql.DB, error) {
			Expect(driverName).To(Equal("sqlite3"))
			return sqlDB, nil
		}
		originalErrorCode := gplog.GetErrorCode()
		DeferCleanup(gplog.SetErrorCode, originalErrorCode)
		gplog.SetErrorCode(0)

		err = vacuumBackupHistoryStandbySyncSnapshot("/tmp/source.db", "/tmp/snapshot.db")

		Expect(errors.Is(err, closeErr)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("close source history db for standby sync snapshot"))
		Expect(gplog.GetErrorCode()).To(Equal(0))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("returns local cleanup errors after success and joins them with sync errors", func() {
		sourcePath := filepath.Join(GinkgoT().TempDir(), backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		cleanupErr := errors.New("cleanup failed")
		backupHistoryStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			return GinkgoT().TempDir(), nil
		}
		backupHistoryStandbySyncRemoveAll = func(path string) error {
			return cleanupErr
		}

		err := withBackupHistoryStandbySyncSnapshot(sourcePath, 0o600, func(string) error {
			return nil
		})
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())

		syncErr := errors.New("sync failed")
		err = withBackupHistoryStandbySyncSnapshot(sourcePath, 0o600, func(string) error {
			return syncErr
		})
		Expect(errors.Is(err, syncErr)).To(BeTrue())
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())
	})

	It("joins snapshot creation and local cleanup errors", func() {
		sourcePath := filepath.Join(GinkgoT().TempDir(), backupHistoryDBName)
		Expect(os.WriteFile(sourcePath, []byte("not sqlite"), 0o600)).To(Succeed())
		cleanupErr := errors.New("cleanup failed")
		backupHistoryStandbySyncRemoveAll = func(path string) error {
			return cleanupErr
		}

		_, tempDir, err := createBackupHistoryStandbySyncSnapshot(sourcePath, 0o600)

		Expect(tempDir).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("VACUUM INTO"))
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())
	})

	It("canonicalizes symlink sources and builds the shared lock path from the canonical source", func() {
		tmpDir := GinkgoT().TempDir()
		realDir := filepath.Join(tmpDir, "real")
		linkDir := filepath.Join(tmpDir, "link")
		Expect(os.Mkdir(realDir, 0o700)).To(Succeed())
		Expect(os.Mkdir(linkDir, 0o700)).To(Succeed())
		// Resolve any symlinks in the OS temp dir itself (e.g. macOS /var -> /private/var)
		// so the expected path matches the canonicalization performed by the code under test.
		canonicalRealDir, err := filepath.EvalSymlinks(realDir)
		Expect(err).ToNot(HaveOccurred())
		realDir = canonicalRealDir
		realSourcePath := filepath.Join(realDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(realSourcePath)
		linkSourcePath := filepath.Join(linkDir, backupHistoryDBName)
		Expect(os.Symlink(realSourcePath, linkSourcePath)).To(Succeed())

		canonicalSourcePath, _, err := canonicalBackupHistoryStandbySyncSource(linkSourcePath)
		Expect(err).ToNot(HaveOccurred())
		Expect(canonicalSourcePath).To(Equal(realSourcePath))
		Expect(backupHistoryStandbySyncLockPath(canonicalSourcePath)).To(Equal(realSourcePath + ".sync.lock"))
	})

	It("rejects a non-regular source", func() {
		sourcePath := GinkgoT().TempDir()

		_, _, err := canonicalBackupHistoryStandbySyncSource(sourcePath)

		Expect(err).To(MatchError(ContainSubstring("is not a regular file")))
	})

	It("skips when no up standby coordinator exists", func() {
		tmpDir := GinkgoT().TempDir()
		sourcePath := filepath.Join(tmpDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		globalFPInfo = backupfilepath.FilePathInfo{SegDirMap: map[int]string{-1: tmpDir}}
		mock := setupBackupHistoryStandbySyncConnection()
		mock.ExpectQuery(regexp.QuoteMeta(backupHistoryStandbySyncStandbySQL)).WillReturnError(sql.ErrNoRows)

		commandCalls := setBackupHistoryStandbySyncCommands(nil)
		skipReason, err := syncBackupHistoryToStandby()

		Expect(err).ToNot(HaveOccurred())
		Expect(skipReason).To(Equal("no up standby coordinator found"))
		Expect(*commandCalls).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("orchestrates discovery, snapshot, rsync transport, atomic install, and local cleanup", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby data")
		snapshotDir := filepath.Join(tmpDir, "snapshot dir")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		Expect(os.Mkdir(standbyDataDir, 0o700)).To(Succeed())
		sourcePath := filepath.Join(primaryDataDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		globalFPInfo = backupfilepath.FilePathInfo{SegDirMap: map[int]string{-1: primaryDataDir}}
		mock := setupBackupHistoryStandbySyncConnection()
		expectBackupHistoryStandbySyncStandby(mock, "sdw-standby", standbyDataDir)
		backupHistoryStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			Expect(dir).To(Equal(""))
			Expect(pattern).To(Equal(backupHistoryStandbySyncTempDirPattern))
			Expect(os.Mkdir(snapshotDir, 0o700)).To(Succeed())
			return snapshotDir, nil
		}

		commandCalls := setBackupHistoryStandbySyncCommands([]backupHistoryStandbySyncCommandResponse{{}, {}})
		start := time.Now()
		Expect(cmdFlags.Set(options.HISTORY_SYNC_STANDBY_TIMEOUT, "600")).To(Succeed())
		skipReason, err := syncBackupHistoryToStandby()
		finished := time.Now()

		Expect(err).ToNot(HaveOccurred())
		Expect(skipReason).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
		Expect(*commandCalls).To(HaveLen(2))
		snapshotPath := filepath.Join(snapshotDir, backupHistoryDBName)
		remoteTempPath := newBackupHistoryStandbySyncRemoteTempPath(standbyDataDir, snapshotPath)
		Expect((*commandCalls)[0].name).To(Equal("rsync"))
		Expect((*commandCalls)[0].args).To(Equal(buildBackupHistoryStandbySyncRsyncArgs(snapshotPath, "sdw-standby", "gpadmin", remoteTempPath)))
		Expect((*commandCalls)[1].name).To(Equal("ssh"))
		Expect((*commandCalls)[1].ctx).To(BeIdenticalTo((*commandCalls)[0].ctx))
		deadline, ok := (*commandCalls)[0].ctx.Deadline()
		Expect(ok).To(BeTrue())
		Expect(deadline).To(BeTemporally(">=", start.Add(600*time.Second)))
		Expect(deadline).To(BeTemporally("<=", finished.Add(600*time.Second)))
		Expect((*commandCalls)[1].args).To(Equal([]string{
			"-o",
			"BatchMode=yes",
			"-o",
			"StrictHostKeyChecking=no",
			"-o",
			"ConnectTimeout=30",
			"gpadmin@sdw-standby",
			buildBackupHistoryStandbySyncRemoteInstallCommand(remoteTempPath, filepath.Join(standbyDataDir, backupHistoryDBName)),
		}))
		_, err = os.Stat(snapshotDir)
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	})

	It("returns the rsync stage, configured seconds, and DeadlineExceeded without waiting", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourcePath := filepath.Join(primaryDataDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		globalFPInfo = backupfilepath.FilePathInfo{SegDirMap: map[int]string{-1: primaryDataDir}}
		mock := setupBackupHistoryStandbySyncConnection()
		expectBackupHistoryStandbySyncStandby(mock, "sdw-standby", standbyDataDir)
		Expect(cmdFlags.Set(options.HISTORY_SYNC_STANDBY_TIMEOUT, "600")).To(Succeed())
		backupHistoryStandbySyncContextWithTimeout = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
			Expect(timeout).To(Equal(600 * time.Second))
			return context.WithDeadline(parent, time.Now().Add(-time.Second))
		}
		commandCalls := setBackupHistoryStandbySyncCommands([]backupHistoryStandbySyncCommandResponse{
			{err: context.DeadlineExceeded},
			{},
		})
		start := time.Now()

		_, err := syncBackupHistoryToStandby()
		finished := time.Now()

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("rsync standby history snapshot"))
		Expect(err.Error()).To(ContainSubstring("timed out after 600 seconds"))
		Expect(*commandCalls).To(HaveLen(2))
		Expect((*commandCalls)[0].ctxErr).To(Equal(context.DeadlineExceeded))
		Expect((*commandCalls)[1].ctx).ToNot(BeIdenticalTo((*commandCalls)[0].ctx))
		Expect((*commandCalls)[1].ctxErr).ToNot(HaveOccurred())
		Expect((*commandCalls)[1].hasDeadline).To(BeTrue())
		Expect((*commandCalls)[1].deadline).To(BeTemporally(">=", start.Add(backupHistoryStandbySyncCleanupTimeout)))
		Expect((*commandCalls)[1].deadline).To(BeTemporally("<=", finished.Add(backupHistoryStandbySyncCleanupTimeout)))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("releases the source lock after transport errors", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourcePath := filepath.Join(primaryDataDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		globalFPInfo = backupfilepath.FilePathInfo{SegDirMap: map[int]string{-1: primaryDataDir}}
		mock := setupBackupHistoryStandbySyncConnection()
		expectBackupHistoryStandbySyncStandby(mock, "sdw-standby", standbyDataDir)
		setBackupHistoryStandbySyncCommands([]backupHistoryStandbySyncCommandResponse{
			{output: []byte("rsync failed"), err: errors.New("exit status 1")},
			{},
		})

		_, err := syncBackupHistoryToStandby()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rsync standby history snapshot"))
		Expect(err.Error()).To(ContainSubstring("rsync failed"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())

		sourceLock, err := lockfile.New(backupHistoryStandbySyncLockPath(sourcePath))
		Expect(err).ToNot(HaveOccurred())
		Expect(sourceLock.TryLock()).To(Succeed())
		Expect(sourceLock.Unlock()).To(Succeed())
	})

	It("returns lock contention as an error without creating a snapshot", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourcePath := filepath.Join(primaryDataDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(sourcePath)
		globalFPInfo = backupfilepath.FilePathInfo{SegDirMap: map[int]string{-1: primaryDataDir}}
		mock := setupBackupHistoryStandbySyncConnection()
		expectBackupHistoryStandbySyncStandby(mock, "sdw-standby", standbyDataDir)
		lockPath := backupHistoryStandbySyncLockPath(sourcePath)
		Expect(os.WriteFile(lockPath, []byte(fmt.Sprintf("%d\n", os.Getppid())), 0o600)).To(Succeed())
		defer os.Remove(lockPath)
		mkdirTempCalls := 0
		backupHistoryStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			mkdirTempCalls++
			return "", errors.New("snapshot should not be created")
		}

		_, err := syncBackupHistoryToStandby()

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("lock standby history sync source"))
		Expect(mkdirTempCalls).To(Equal(0))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("protects rsync paths and quotes remote shell paths", func() {
		remoteTempPath := "/data dir/standby's/.gpbackup_history.db.tmp"
		destPath := "/data dir/standby's/gpbackup_history.db"
		Expect(backupHistoryStandbySyncSSHOptions).To(ContainSubstring("BatchMode=yes"))

		Expect(buildBackupHistoryStandbySyncRsyncArgs("/tmp/snapshot", "sdw-standby", "gpadmin", remoteTempPath)).To(Equal([]string{
			"-p",
			"-s",
			"-e",
			backupHistoryStandbySyncSSHOptions,
			"--",
			"/tmp/snapshot",
			"gpadmin@sdw-standby:" + remoteTempPath,
		}))
		installCommand := buildBackupHistoryStandbySyncRemoteInstallCommand(remoteTempPath, destPath)
		Expect(installCommand).To(ContainSubstring("test -f " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("chown --reference=" + shellQuoteBackupHistoryStandbySyncPath(destPath) + " -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("chmod --reference=" + shellQuoteBackupHistoryStandbySyncPath(destPath) + " -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("mv -f -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath) + " " + shellQuoteBackupHistoryStandbySyncPath(destPath)))
		Expect(buildBackupHistoryStandbySyncRemoteCleanupCommand(remoteTempPath)).To(Equal("rm -f -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
	})

	It("keeps protected rsync paths out of the remote shell command", func() {
		rsyncPath := requireBackupHistoryStandbySyncRsync3()
		tmpDir := GinkgoT().TempDir()
		remoteArgsPath := filepath.Join(tmpDir, "remote-args")
		fakeShellPath := filepath.Join(tmpDir, "fake-ssh")
		fakeShell := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RSYNC_REMOTE_ARGS_FILE\"\nexit 1\n"
		Expect(os.WriteFile(fakeShellPath, []byte(fakeShell), 0o700)).To(Succeed())

		snapshotPath := filepath.Join(tmpDir, "snapshot")
		Expect(os.WriteFile(snapshotPath, []byte("snapshot"), 0o600)).To(Succeed())
		remoteTempPath := "/data dir/standby's/$HOME/[history]*;RSYNC_REMOTE_PATH_SENTINEL"
		args := buildBackupHistoryStandbySyncRsyncArgs(snapshotPath, "sdw-standby", "gpadmin", remoteTempPath)
		remoteShellReplaced := false
		for i := range args {
			if args[i] == "-e" && i+1 < len(args) {
				args[i+1] = fakeShellPath
				remoteShellReplaced = true
				break
			}
		}
		Expect(remoteShellReplaced).To(BeTrue())

		command := exec.Command(rsyncPath, args...)
		command.Env = append(os.Environ(), "RSYNC_REMOTE_ARGS_FILE="+remoteArgsPath)
		_, err := command.CombinedOutput()
		Expect(err).To(HaveOccurred())

		remoteArgs, err := os.ReadFile(remoteArgsPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(remoteArgs)).To(ContainSubstring("--server"))
		Expect(string(remoteArgs)).ToNot(ContainSubstring("RSYNC_REMOTE_PATH_SENTINEL"))
	})

	It("chains remote cleanup errors onto the primary transport error", func() {
		cleanupCommandErr := fmt.Errorf("cleanup timeout: %w", context.DeadlineExceeded)
		commandCalls := setBackupHistoryStandbySyncCommands([]backupHistoryStandbySyncCommandResponse{
			{output: []byte("cleanup failed"), err: cleanupCommandErr},
		})
		primaryErr := errors.New("install failed")
		start := time.Now()

		err := cleanupBackupHistoryStandbySyncRemoteTempAfterError(primaryErr, "sdw-standby", "gpadmin", "/standby/.tmp")
		finished := time.Now()

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, primaryErr)).To(BeTrue())
		Expect(errors.Is(err, cleanupCommandErr)).To(BeTrue())
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("install failed"))
		Expect(err.Error()).To(ContainSubstring("additionally failed to clean up remote temp file"))
		Expect(err.Error()).To(ContainSubstring("cleanup failed"))
		Expect(*commandCalls).To(HaveLen(1))
		Expect((*commandCalls)[0].name).To(Equal("ssh"))
		Expect((*commandCalls)[0].ctxErr).ToNot(HaveOccurred())
		Expect((*commandCalls)[0].hasDeadline).To(BeTrue())
		Expect((*commandCalls)[0].deadline).To(BeTemporally(">=", start.Add(backupHistoryStandbySyncCleanupTimeout)))
		Expect((*commandCalls)[0].deadline).To(BeTemporally("<=", finished.Add(backupHistoryStandbySyncCleanupTimeout)))
	})

	It("logs disabled automatic sync without invoking discovery", func() {
		stdout, _, _ := testhelper.SetupTestLogger()
		syncCalls := 0
		backupHistoryStandbySync = func() (string, error) {
			syncCalls++
			return "", errors.New("sync should not run")
		}

		skipReason, err := syncBackupHistoryToStandbyBestEffort(true)

		Expect(err).ToNot(HaveOccurred())
		Expect(skipReason).To(Equal("disabled by --" + options.NO_HISTORY_SYNC_STANDBY))
		Expect(syncCalls).To(Equal(0))
		Expect(string(stdout.Contents())).To(ContainSubstring("Skipping history db sync to standby coordinator: disabled by --" + options.NO_HISTORY_SYNC_STANDBY))
	})

	It("warns automatic sync failures without exiting", func() {
		stdout, _, _ := testhelper.SetupTestLogger()
		originalErrorCode := gplog.GetErrorCode()
		DeferCleanup(gplog.SetErrorCode, originalErrorCode)
		gplog.SetErrorCode(0)
		backupHistoryStandbySync = func() (string, error) {
			return "", fmt.Errorf("standby history sync transport timed out after 300 seconds: %w", context.DeadlineExceeded)
		}

		_, err := syncBackupHistoryToStandbyBestEffort(false)

		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, context.DeadlineExceeded)).To(BeTrue())
		Expect(string(stdout.Contents())).To(ContainSubstring("History db sync to standby coordinator failed; standby history may be stale: standby history sync transport timed out after 300 seconds"))
		Expect(gplog.GetErrorCode()).To(Equal(0))
	})

	It("runs automatic sync only after successful cleanup history update for successful backups", func() {
		calls := 0
		disabledValues := make([]bool, 0)
		originalBestEffort := backupHistoryStandbySync
		backupHistoryStandbySync = func() (string, error) {
			calls++
			return "", nil
		}
		defer func() {
			backupHistoryStandbySync = originalBestEffort
		}()
		backupHistoryStandbySyncCommandExec = func(ctx context.Context, name string, args ...string) backupHistoryStandbySyncCommand {
			return backupHistoryStandbySyncFakeCommand{}
		}

		syncBackupHistoryToStandbyAfterCleanup(false, true)
		Expect(calls).To(Equal(1))

		syncBackupHistoryToStandbyAfterCleanup(true, true)
		syncBackupHistoryToStandbyAfterCleanup(false, false)
		Expect(cmdFlags.Set(options.NO_HISTORY, "true")).To(Succeed())
		syncBackupHistoryToStandbyAfterCleanup(false, true)
		Expect(calls).To(Equal(1))

		Expect(cmdFlags.Set(options.NO_HISTORY, "false")).To(Succeed())
		Expect(cmdFlags.Set(options.NO_HISTORY_SYNC_STANDBY, "true")).To(Succeed())
		backupHistoryStandbySync = func() (string, error) {
			calls++
			disabledValues = append(disabledValues, true)
			return "", nil
		}
		syncBackupHistoryToStandbyAfterCleanup(false, true)
		Expect(calls).To(Equal(1))
		Expect(disabledValues).To(BeEmpty())
	})
})

func requireBackupHistoryStandbySyncRsync3() string {
	GinkgoHelper()

	rsyncPath, err := exec.LookPath("rsync")
	if err != nil {
		Skip("rsync is not installed")
		return ""
	}
	output, err := exec.Command(rsyncPath, "--version").CombinedOutput()
	if err != nil {
		Skip(fmt.Sprintf("cannot determine rsync version: %v", err))
		return ""
	}
	match := regexp.MustCompile(`(?m)^rsync\s+version\s+([0-9]+)\.`).FindStringSubmatch(string(output))
	if len(match) != 2 {
		Skip("cannot parse rsync version")
		return ""
	}
	majorVersion, err := strconv.Atoi(match[1])
	if err != nil || majorVersion < 3 {
		Skip("rsync 3.0.0 or later is required")
		return ""
	}
	return rsyncPath
}

func createBackupHistoryStandbySyncSQLiteDB(path string) {
	db, err := sql.Open("sqlite3", backupHistoryStandbySyncSQLiteURI(path, "rwc"))
	Expect(err).ToNot(HaveOccurred())
	defer db.Close()

	_, err = db.Exec("CREATE TABLE sync_test (id INTEGER PRIMARY KEY, value TEXT)")
	Expect(err).ToNot(HaveOccurred())
	_, err = db.Exec("INSERT INTO sync_test (value) VALUES ('present')")
	Expect(err).ToNot(HaveOccurred())
}

func setupBackupHistoryStandbySyncConnection() sqlmock.Sqlmock {
	sqlDB, mock, err := sqlmock.New()
	Expect(err).ToNot(HaveOccurred())
	connectionPool = &dbconn.DBConn{
		ConnPool: []*sqlx.DB{sqlx.NewDb(sqlDB, "sqlmock")},
		NumConns: 1,
		Tx:       []*sqlx.Tx{nil},
	}
	return mock
}

func expectBackupHistoryStandbySyncStandby(mock sqlmock.Sqlmock, host, dataDir string) {
	mock.ExpectQuery(regexp.QuoteMeta(backupHistoryStandbySyncStandbySQL)).
		WillReturnRows(sqlmock.NewRows([]string{"hostname", "datadir"}).AddRow(host, dataDir))
}

func setBackupHistoryStandbySyncCommands(responses []backupHistoryStandbySyncCommandResponse) *[]backupHistoryStandbySyncCommandCall {
	calls := make([]backupHistoryStandbySyncCommandCall, 0)
	backupHistoryStandbySyncCommandExec = func(ctx context.Context, name string, args ...string) backupHistoryStandbySyncCommand {
		deadline, hasDeadline := ctx.Deadline()
		calls = append(calls, backupHistoryStandbySyncCommandCall{
			ctx:         ctx,
			ctxErr:      ctx.Err(),
			deadline:    deadline,
			hasDeadline: hasDeadline,
			name:        name,
			args:        append([]string{}, args...),
		})
		response := backupHistoryStandbySyncCommandResponse{}
		if len(calls) <= len(responses) {
			response = responses[len(calls)-1]
		}
		return backupHistoryStandbySyncFakeCommand{output: response.output, err: response.err}
	}
	return &calls
}
