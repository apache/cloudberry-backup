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
	"os"
	"path/filepath"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"
	backupfilepath "github.com/apache/cloudberry-backup/filepath"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"
	"github.com/jmoiron/sqlx"
	"github.com/nightlyone/lockfile"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type backupHistoryStandbySyncCommandCall struct {
	name string
	args []string
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
	var originalSync func() (string, error)

	BeforeEach(func() {
		testhelper.SetupTestLogger()
		cmdFlags = pflag.NewFlagSet("gpbackup", pflag.ContinueOnError)
		options.SetBackupFlagDefaults(cmdFlags)
		globalFPInfo = backupfilepath.FilePathInfo{}
		connectionPool = nil
		originalSync = backupHistoryStandbySync
		backupHistoryStandbySync = syncBackupHistoryToStandby
		backupHistoryStandbySyncCommandExec = func(name string, args ...string) backupHistoryStandbySyncCommand {
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
		backupHistoryStandbySyncCommandExec = func(name string, args ...string) backupHistoryStandbySyncCommand {
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

	It("canonicalizes symlink sources and builds the shared lock path from the canonical source", func() {
		tmpDir := GinkgoT().TempDir()
		realDir := filepath.Join(tmpDir, "real")
		linkDir := filepath.Join(tmpDir, "link")
		Expect(os.Mkdir(realDir, 0o700)).To(Succeed())
		Expect(os.Mkdir(linkDir, 0o700)).To(Succeed())
		realSourcePath := filepath.Join(realDir, backupHistoryDBName)
		createBackupHistoryStandbySyncSQLiteDB(realSourcePath)
		linkSourcePath := filepath.Join(linkDir, backupHistoryDBName)
		Expect(os.Symlink(realSourcePath, linkSourcePath)).To(Succeed())

		canonicalSourcePath, _, err := canonicalBackupHistoryStandbySyncSource(linkSourcePath)
		Expect(err).ToNot(HaveOccurred())
		Expect(canonicalSourcePath).To(Equal(realSourcePath))
		Expect(backupHistoryStandbySyncLockPath(canonicalSourcePath)).To(Equal(realSourcePath + ".sync.lock"))
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
		skipReason, err := syncBackupHistoryToStandby()

		Expect(err).ToNot(HaveOccurred())
		Expect(skipReason).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
		Expect(*commandCalls).To(HaveLen(2))
		snapshotPath := filepath.Join(snapshotDir, backupHistoryDBName)
		remoteTempPath := newBackupHistoryStandbySyncRemoteTempPath(standbyDataDir, snapshotPath)
		Expect((*commandCalls)[0].name).To(Equal("rsync"))
		Expect((*commandCalls)[0].args).To(Equal(buildBackupHistoryStandbySyncRsyncArgs(snapshotPath, "sdw-standby", "gpadmin", remoteTempPath)))
		Expect((*commandCalls)[1].name).To(Equal("ssh"))
		Expect((*commandCalls)[1].args).To(Equal([]string{
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

	It("quotes remote shell paths in rsync, install, and cleanup commands", func() {
		remoteTempPath := "/data dir/standby's/.gpbackup_history.db.tmp"
		destPath := "/data dir/standby's/gpbackup_history.db"

		Expect(buildBackupHistoryStandbySyncRsyncArgs("/tmp/snapshot", "sdw-standby", "gpadmin", remoteTempPath)).To(Equal([]string{
			"-p",
			"-e",
			backupHistoryStandbySyncSSHOptions,
			"--",
			"/tmp/snapshot",
			"gpadmin@sdw-standby:" + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath),
		}))
		installCommand := buildBackupHistoryStandbySyncRemoteInstallCommand(remoteTempPath, destPath)
		Expect(installCommand).To(ContainSubstring("test -f " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("chown --reference=" + shellQuoteBackupHistoryStandbySyncPath(destPath) + " -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("chmod --reference=" + shellQuoteBackupHistoryStandbySyncPath(destPath) + " -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("mv -f -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath) + " " + shellQuoteBackupHistoryStandbySyncPath(destPath)))
		Expect(buildBackupHistoryStandbySyncRemoteCleanupCommand(remoteTempPath)).To(Equal("rm -f -- " + shellQuoteBackupHistoryStandbySyncPath(remoteTempPath)))
	})

	It("chains remote cleanup errors onto the primary transport error", func() {
		commandCalls := setBackupHistoryStandbySyncCommands([]backupHistoryStandbySyncCommandResponse{
			{output: []byte("cleanup failed"), err: errors.New("exit status 255")},
		})
		primaryErr := errors.New("install failed")

		err := cleanupBackupHistoryStandbySyncRemoteTempAfterError(primaryErr, "sdw-standby", "gpadmin", "/standby/.tmp")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("install failed"))
		Expect(err.Error()).To(ContainSubstring("additionally failed to clean up remote temp file"))
		Expect(err.Error()).To(ContainSubstring("cleanup failed"))
		Expect(*commandCalls).To(HaveLen(1))
		Expect((*commandCalls)[0].name).To(Equal("ssh"))
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
		backupHistoryStandbySync = func() (string, error) {
			return "", errors.New("transport failed")
		}

		_, err := syncBackupHistoryToStandbyBestEffort(false)

		Expect(err).To(HaveOccurred())
		Expect(string(stdout.Contents())).To(ContainSubstring("History db sync to standby coordinator failed; standby history may be stale: transport failed"))
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
		backupHistoryStandbySyncCommandExec = func(name string, args ...string) backupHistoryStandbySyncCommand {
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
	backupHistoryStandbySyncCommandExec = func(name string, args ...string) backupHistoryStandbySyncCommand {
		calls = append(calls, backupHistoryStandbySyncCommandCall{name: name, args: append([]string{}, args...)})
		response := backupHistoryStandbySyncCommandResponse{}
		if len(calls) <= len(responses) {
			response = responses[len(calls)-1]
		}
		return backupHistoryStandbySyncFakeCommand{output: response.output, err: response.err}
	}
	return &calls
}
