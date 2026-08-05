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
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-go-libs/testhelper"
	"github.com/jmoiron/sqlx"
	"github.com/nightlyone/lockfile"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	historyStandbySyncPrimarySQL = "SELECT datadir FROM gp_segment_configuration WHERE content = -1 AND role = 'p' AND status = 'u';"
	historyStandbySyncStandbySQL = "SELECT hostname, datadir FROM gp_segment_configuration WHERE content = -1 AND role = 'm' AND status = 'u';"
)

type historyStandbySyncCommandCall struct {
	name string
	args []string
}

type historyStandbySyncCommandResponse struct {
	output []byte
	err    error
}

type historyStandbySyncFakeCommand struct {
	output []byte
	err    error
}

func (c historyStandbySyncFakeCommand) CombinedOutput() ([]byte, error) {
	return c.output, c.err
}

var _ = Describe("history standby sync", func() {
	var (
		originalHistoryStandbySync                func() historyStandbySyncResult
		originalOpenClusterConn                   func() (*sqlx.DB, error)
		originalOpenSQLite                        func(string, string) (*sql.DB, error)
		originalMkdirTemp                         func(string, string) (string, error)
		originalRemoveAll                         func(string) error
		originalNow                               func() time.Time
		originalPID                               func() int
		originalCurrentUser                       func() (string, error)
		originalRunSSHCommand                     func(string, string, string) ([]byte, error)
		originalExecCombinedOutputCommand         func(string, ...string) combinedOutputCommand
		savedRootHistoryDB                        string
		savedRootAutoLoadHistoryDB                bool
		savedHistoryStandbySyncEnvironment        map[string]string
		savedHistoryStandbySyncEnvironmentPresent map[string]bool
	)

	BeforeEach(func() {
		testhelper.SetupTestLogger()
		originalHistoryStandbySync = historyStandbySync
		originalOpenClusterConn = historyStandbySyncOpenClusterConn
		originalOpenSQLite = historyStandbySyncOpenSQLite
		originalMkdirTemp = historyStandbySyncMkdirTemp
		originalRemoveAll = historyStandbySyncRemoveAll
		originalNow = historyStandbySyncNow
		originalPID = historyStandbySyncPID
		originalCurrentUser = historyStandbySyncCurrentUser
		originalRunSSHCommand = historyStandbySyncRunSSHCommand
		originalExecCombinedOutputCommand = execCombinedOutputCommand
		savedRootHistoryDB = rootHistoryDB
		savedRootAutoLoadHistoryDB = rootAutoLoadHistoryDB
		savedHistoryStandbySyncEnvironment = make(map[string]string)
		savedHistoryStandbySyncEnvironmentPresent = make(map[string]bool)
		for _, name := range append(historyDBEnvVars, "PGDATABASE") {
			value, ok := os.LookupEnv(name)
			savedHistoryStandbySyncEnvironment[name] = value
			savedHistoryStandbySyncEnvironmentPresent[name] = ok
			Expect(os.Unsetenv(name)).To(Succeed())
		}

		rootHistoryDB = ""
		rootAutoLoadHistoryDB = false
		historyStandbySync = syncHistoryStandby
		historyStandbySyncOpenClusterConn = func() (*sqlx.DB, error) {
			return nil, errors.New("cluster connection was not expected")
		}
		historyStandbySyncOpenSQLite = sql.Open
		historyStandbySyncMkdirTemp = os.MkdirTemp
		historyStandbySyncRemoveAll = os.RemoveAll
		historyStandbySyncNow = func() time.Time {
			return time.Date(2026, 7, 28, 16, 0, 0, 0, time.UTC)
		}
		historyStandbySyncPID = func() int {
			return 4242
		}
		historyStandbySyncCurrentUser = func() (string, error) {
			return "gpadmin", nil
		}
		historyStandbySyncRunSSHCommand = runHistoryStandbySyncSSHCommand
		execCombinedOutputCommand = func(name string, args ...string) combinedOutputCommand {
			return historyStandbySyncFakeCommand{}
		}
	})

	AfterEach(func() {
		historyStandbySync = originalHistoryStandbySync
		historyStandbySyncOpenClusterConn = originalOpenClusterConn
		historyStandbySyncOpenSQLite = originalOpenSQLite
		historyStandbySyncMkdirTemp = originalMkdirTemp
		historyStandbySyncRemoveAll = originalRemoveAll
		historyStandbySyncNow = originalNow
		historyStandbySyncPID = originalPID
		historyStandbySyncCurrentUser = originalCurrentUser
		historyStandbySyncRunSSHCommand = originalRunSSHCommand
		execCombinedOutputCommand = originalExecCombinedOutputCommand
		rootHistoryDB = savedRootHistoryDB
		rootAutoLoadHistoryDB = savedRootAutoLoadHistoryDB
		for name, value := range savedHistoryStandbySyncEnvironment {
			if savedHistoryStandbySyncEnvironmentPresent[name] {
				Expect(os.Setenv(name, value)).To(Succeed())
			} else {
				Expect(os.Unsetenv(name)).To(Succeed())
			}
		}
	})

	It("skips default and unresolved auto-loaded history db sources before discovery", func() {
		sourceDBPath, skipReason := getHistoryStandbySyncSourceDBPath()
		Expect(sourceDBPath).To(Equal(historyDBNameConst))
		Expect(skipReason).To(Equal("using default working-directory history db"))

		rootAutoLoadHistoryDB = true
		sourceDBPath, skipReason = getHistoryStandbySyncSourceDBPath()
		Expect(sourceDBPath).To(Equal(historyDBNameConst))
		Expect(skipReason).To(Equal("--auto-load-history-db did not resolve the cluster history db"))
	})

	It("uses auto-load history db path resolved from coordinator data directory", func() {
		rootAutoLoadHistoryDB = true
		Expect(os.Setenv("COORDINATOR_DATA_DIRECTORY", "/coordinator/data")).To(Succeed())

		sourceDBPath, skipReason := getHistoryStandbySyncSourceDBPath()

		Expect(sourceDBPath).To(Equal(filepath.Join("/coordinator/data", historyDBNameConst)))
		Expect(skipReason).To(BeEmpty())
	})

	It("creates a verified snapshot with source contents and permissions", func() {
		tmpDir := GinkgoT().TempDir()
		sourceDBPath := filepath.Join(tmpDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		Expect(os.Chmod(sourceDBPath, 0o640)).To(Succeed())

		snapshotPath, tempDir, err := createHistoryStandbySyncSnapshot(sourceDBPath, 0o640)
		Expect(err).ToNot(HaveOccurred())
		defer cleanupHistoryStandbySyncTempDir(tempDir)

		Expect(snapshotPath).To(Equal(filepath.Join(tempDir, historyDBNameConst)))
		snapshotInfo, err := os.Stat(snapshotPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshotInfo.Mode().Perm()).To(Equal(os.FileMode(0o640)))

		snapshotDB, err := sql.Open("sqlite3", historyStandbySyncSQLiteURI(snapshotPath, "ro"))
		Expect(err).ToNot(HaveOccurred())
		defer snapshotDB.Close()
		var value string
		Expect(snapshotDB.QueryRow("SELECT value FROM sync_test WHERE id = 1").Scan(&value)).To(Succeed())
		Expect(value).To(Equal("present"))
		Expect(validateHistoryStandbySyncSnapshot(snapshotPath)).To(Succeed())
	})

	It("rejects corrupted SQLite sources before transport and removes the temp directory", func() {
		tmpDir := GinkgoT().TempDir()
		sourceDBPath := filepath.Join(tmpDir, historyDBNameConst)
		Expect(os.WriteFile(sourceDBPath, []byte("not sqlite"), 0o600)).To(Succeed())
		snapshotDir := filepath.Join(tmpDir, "snapshot")
		historyStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			Expect(os.Mkdir(snapshotDir, 0o700)).To(Succeed())
			return snapshotDir, nil
		}

		_, tempDir, err := createHistoryStandbySyncSnapshot(sourceDBPath, 0o600)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("VACUUM INTO"))
		Expect(tempDir).To(BeEmpty())
		_, statErr := os.Stat(snapshotDir)
		Expect(errors.Is(statErr, os.ErrNotExist)).To(BeTrue())
	})

	It("returns SQLite close errors from snapshot validation", func() {
		sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
		closeErr := errors.New("close failed")
		mock.ExpectQuery("PRAGMA quick_check").
			WillReturnRows(sqlmock.NewRows([]string{"quick_check"}).AddRow("ok"))
		mock.ExpectClose().WillReturnError(closeErr)
		historyStandbySyncOpenSQLite = func(driverName, dataSourceName string) (*sql.DB, error) {
			Expect(driverName).To(Equal("sqlite3"))
			return sqlDB, nil
		}

		results, err := runHistoryStandbySyncQuickCheck("/tmp/snapshot.db")

		Expect(results).To(Equal([]string{"ok"}))
		Expect(errors.Is(err, closeErr)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("close standby history sync snapshot"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("returns local cleanup errors after success and joins them with sync errors", func() {
		sourceDBPath := filepath.Join(GinkgoT().TempDir(), historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		cleanupErr := errors.New("cleanup failed")
		historyStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			return GinkgoT().TempDir(), nil
		}
		historyStandbySyncRemoveAll = func(path string) error {
			return cleanupErr
		}

		err := withHistoryStandbySyncSnapshot(sourceDBPath, 0o600, func(string) error {
			return nil
		})
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())

		syncErr := errors.New("sync failed")
		err = withHistoryStandbySyncSnapshot(sourceDBPath, 0o600, func(string) error {
			return syncErr
		})
		Expect(errors.Is(err, syncErr)).To(BeTrue())
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())
	})

	It("joins snapshot creation and local cleanup errors", func() {
		sourceDBPath := filepath.Join(GinkgoT().TempDir(), historyDBNameConst)
		Expect(os.WriteFile(sourceDBPath, []byte("not sqlite"), 0o600)).To(Succeed())
		cleanupErr := errors.New("cleanup failed")
		historyStandbySyncRemoveAll = func(path string) error {
			return cleanupErr
		}

		_, tempDir, err := createHistoryStandbySyncSnapshot(sourceDBPath, 0o600)

		Expect(tempDir).To(BeEmpty())
		Expect(err.Error()).To(ContainSubstring("VACUUM INTO"))
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())
	})

	It("returns discovery connection close errors", func() {
		primaryDataDir := GinkgoT().TempDir()
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		sqlDB, mock, err := sqlmock.New()
		Expect(err).ToNot(HaveOccurred())
		mock.ExpectQuery(regexp.QuoteMeta(historyStandbySyncPrimarySQL)).
			WillReturnRows(sqlmock.NewRows([]string{"datadir"}).AddRow(primaryDataDir))
		mock.ExpectQuery(regexp.QuoteMeta(historyStandbySyncStandbySQL)).
			WillReturnRows(sqlmock.NewRows([]string{"hostname", "datadir"}).AddRow("sdw-standby", "/data/standby"))
		closeErr := errors.New("close failed")
		mock.ExpectClose().WillReturnError(closeErr)
		historyStandbySyncOpenClusterConn = func() (*sqlx.DB, error) {
			return sqlx.NewDb(sqlDB, "sqlmock"), nil
		}

		target, skipReason, err := discoverHistoryStandbySyncTarget(sourceDBPath)

		Expect(target).ToNot(BeNil())
		Expect(skipReason).To(BeEmpty())
		Expect(errors.Is(err, closeErr)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("close local cluster connection for standby history sync discovery"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("canonicalizes symlink sources and uses the shared lock path suffix", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		// Resolve any symlinks in the OS temp dir itself (e.g. macOS /var -> /private/var)
		// so the expected path matches the canonicalization performed by the code under test.
		canonicalPrimaryDataDir, err := filepath.EvalSymlinks(primaryDataDir)
		Expect(err).ToNot(HaveOccurred())
		primaryDataDir = canonicalPrimaryDataDir
		realSourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(realSourceDBPath)
		linkSourceDBPath := filepath.Join(tmpDir, "history-link.db")
		Expect(os.Symlink(realSourceDBPath, linkSourceDBPath)).To(Succeed())

		canonicalSourceDBPath, _, err := canonicalHistoryStandbySyncSource(linkSourceDBPath)

		Expect(err).ToNot(HaveOccurred())
		Expect(canonicalSourceDBPath).To(Equal(realSourceDBPath))
		Expect(historyStandbySyncLockPath(canonicalSourceDBPath)).To(Equal(realSourceDBPath + ".sync.lock"))
	})

	It("rejects custom history db paths after primary datadir discovery", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		customDataDir := filepath.Join(tmpDir, "custom")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		Expect(os.Mkdir(customDataDir, 0o700)).To(Succeed())
		createHistoryStandbySyncSQLiteDB(filepath.Join(primaryDataDir, historyDBNameConst))
		customSourceDBPath := filepath.Join(customDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(customSourceDBPath)
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, "", nil, false)

		target, skipReason, err := discoverHistoryStandbySyncTarget(customSourceDBPath)

		Expect(err).ToNot(HaveOccurred())
		Expect(target).To(BeNil())
		Expect(skipReason).To(ContainSubstring("is not cluster history db"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("rejects non-regular source history db files", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		Expect(os.MkdirAll(sourceDBPath, 0o700)).To(Succeed())
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, "", nil, false)

		target, skipReason, err := discoverHistoryStandbySyncTarget(sourceDBPath)

		Expect(target).To(BeNil())
		Expect(skipReason).To(BeEmpty())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("not a regular file"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("skips when no up standby coordinator exists", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		rootHistoryDB = sourceDBPath
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, "", sql.ErrNoRows, true)
		commandCalls := setHistoryStandbySyncCommands(nil)

		result := syncHistoryStandby()

		Expect(result.err).ToNot(HaveOccurred())
		Expect(result.skipReason).To(Equal("no up standby coordinator found"))
		Expect(*commandCalls).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("orchestrates discovery, lock, snapshot, rsync transport, atomic install, and local cleanup", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby data")
		snapshotDir := filepath.Join(tmpDir, "snapshot dir")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		rootHistoryDB = sourceDBPath
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, standbyDataDir, nil, true)
		historyStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			Expect(dir).To(Equal(""))
			Expect(pattern).To(Equal("gpbackman-history-standby-sync-20260728160000-4242-*"))
			Expect(os.Mkdir(snapshotDir, 0o700)).To(Succeed())
			return snapshotDir, nil
		}
		commandCalls := setHistoryStandbySyncCommands([]historyStandbySyncCommandResponse{{}, {}})

		result := syncHistoryStandby()

		Expect(result.err).ToNot(HaveOccurred())
		Expect(result.skipReason).To(BeEmpty())
		Expect(mock.ExpectationsWereMet()).To(Succeed())
		Expect(*commandCalls).To(HaveLen(2))
		snapshotPath := filepath.Join(snapshotDir, historyDBNameConst)
		remoteTempPath := newHistoryStandbySyncRemoteTempPath(standbyDataDir, snapshotPath)
		Expect((*commandCalls)[0].name).To(Equal("rsync"))
		Expect((*commandCalls)[0].args).To(Equal(buildHistoryStandbySyncRsyncArgs(snapshotPath, "sdw-standby", "gpadmin", remoteTempPath)))
		Expect((*commandCalls)[1].name).To(Equal("ssh"))
		Expect((*commandCalls)[1].args).To(Equal([]string{
			"-o",
			"StrictHostKeyChecking=no",
			"-o",
			"ConnectTimeout=30",
			"gpadmin@sdw-standby",
			buildHistoryStandbySyncRemoteInstallCommand(remoteTempPath, filepath.Join(standbyDataDir, historyDBNameConst)),
		}))
		_, err := os.Stat(snapshotDir)
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	})

	It("uses the default cluster discovery connection for PGDATABASE resolution", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		rootHistoryDB = sourceDBPath
		Expect(os.Setenv("PGDATABASE", "template1")).To(Succeed())
		openCalls := 0
		mock := setupHistoryStandbySyncClusterConnWithHook(primaryDataDir, filepath.Join(tmpDir, "standby"), nil, true, func(db *sqlx.DB) (*sqlx.DB, error) {
			openCalls++
			Expect(os.Getenv("PGDATABASE")).To(Equal("template1"))
			return db, nil
		})
		setHistoryStandbySyncCommands([]historyStandbySyncCommandResponse{{}, {}})

		result := syncHistoryStandby()

		Expect(result.err).ToNot(HaveOccurred())
		Expect(openCalls).To(Equal(1))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("releases the source lock after transport errors", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		rootHistoryDB = sourceDBPath
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, standbyDataDir, nil, true)
		setHistoryStandbySyncCommands([]historyStandbySyncCommandResponse{
			{output: []byte("rsync failed"), err: errors.New("exit status 1")},
			{},
		})

		result := syncHistoryStandby()

		Expect(result.err).To(HaveOccurred())
		Expect(result.err.Error()).To(ContainSubstring("rsync standby history snapshot"))
		Expect(result.err.Error()).To(ContainSubstring("rsync failed"))
		Expect(mock.ExpectationsWereMet()).To(Succeed())

		sourceLock, err := lockfile.New(historyStandbySyncLockPath(sourceDBPath))
		Expect(err).ToNot(HaveOccurred())
		Expect(sourceLock.TryLock()).To(Succeed())
		Expect(sourceLock.Unlock()).To(Succeed())
	})

	It("returns lock contention as an error without creating a snapshot", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		rootHistoryDB = sourceDBPath
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, standbyDataDir, nil, true)
		lockPath := historyStandbySyncLockPath(sourceDBPath)
		Expect(os.WriteFile(lockPath, []byte(fmt.Sprintf("%d\n", os.Getppid())), 0o600)).To(Succeed())
		defer os.Remove(lockPath)
		mkdirTempCalls := 0
		historyStandbySyncMkdirTemp = func(dir, pattern string) (string, error) {
			mkdirTempCalls++
			return "", errors.New("snapshot should not be created")
		}

		result := syncHistoryStandby()

		Expect(result.err).To(HaveOccurred())
		Expect(result.err.Error()).To(ContainSubstring("lock standby history sync source"))
		Expect(mkdirTempCalls).To(Equal(0))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("quotes remote shell paths in rsync, install, and cleanup commands", func() {
		remoteTempPath := "/data dir/standby's/.gpbackup_history.db.tmp"
		destPath := "/data dir/standby's/gpbackup_history.db"

		Expect(buildHistoryStandbySyncRsyncArgs("/tmp/snapshot", "sdw-standby", "gpadmin", remoteTempPath)).To(Equal([]string{
			"-p",
			"-e",
			historyStandbySyncSSHOptions,
			"--",
			"/tmp/snapshot",
			"gpadmin@sdw-standby:" + shellQuoteHistoryStandbySyncPath(remoteTempPath),
		}))
		installCommand := buildHistoryStandbySyncRemoteInstallCommand(remoteTempPath, destPath)
		Expect(installCommand).To(ContainSubstring("test -f " + shellQuoteHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("chown --reference=" + shellQuoteHistoryStandbySyncPath(destPath) + " -- " + shellQuoteHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("chmod --reference=" + shellQuoteHistoryStandbySyncPath(destPath) + " -- " + shellQuoteHistoryStandbySyncPath(remoteTempPath)))
		Expect(installCommand).To(ContainSubstring("mv -f -- " + shellQuoteHistoryStandbySyncPath(remoteTempPath) + " " + shellQuoteHistoryStandbySyncPath(destPath)))
		Expect(buildHistoryStandbySyncRemoteCleanupCommand(remoteTempPath)).To(Equal("rm -f -- " + shellQuoteHistoryStandbySyncPath(remoteTempPath)))
	})

	It("cleans the concrete remote temp file after install errors", func() {
		tmpDir := GinkgoT().TempDir()
		primaryDataDir := filepath.Join(tmpDir, "primary")
		standbyDataDir := filepath.Join(tmpDir, "standby")
		Expect(os.Mkdir(primaryDataDir, 0o700)).To(Succeed())
		sourceDBPath := filepath.Join(primaryDataDir, historyDBNameConst)
		createHistoryStandbySyncSQLiteDB(sourceDBPath)
		rootHistoryDB = sourceDBPath
		mock := setupHistoryStandbySyncClusterConn(primaryDataDir, standbyDataDir, nil, true)
		commandCalls := setHistoryStandbySyncCommands([]historyStandbySyncCommandResponse{
			{},
			{output: []byte("install failed"), err: errors.New("exit status 1")},
			{},
		})

		result := syncHistoryStandby()

		Expect(result.err).To(HaveOccurred())
		Expect(result.err.Error()).To(ContainSubstring("install standby history snapshot"))
		Expect(result.err.Error()).To(ContainSubstring("install failed"))
		Expect(*commandCalls).To(HaveLen(3))
		Expect((*commandCalls)[2].name).To(Equal("ssh"))
		Expect((*commandCalls)[2].args[len((*commandCalls)[2].args)-1]).To(HavePrefix("rm -f -- "))
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("chains remote cleanup errors onto the primary transport error", func() {
		historyStandbySyncRunSSHCommand = func(remoteCommand, standbyHost, userName string) ([]byte, error) {
			Expect(remoteCommand).To(Equal(buildHistoryStandbySyncRemoteCleanupCommand("/standby/.tmp")))
			Expect(standbyHost).To(Equal("sdw-standby"))
			Expect(userName).To(Equal("gpadmin"))
			return []byte("cleanup failed"), errors.New("exit status 255")
		}
		primaryErr := errors.New("install failed")

		err := cleanupHistoryStandbySyncRemoteTempAfterError(primaryErr, "sdw-standby", "gpadmin", "/standby/.tmp")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("install failed"))
		Expect(err.Error()).To(ContainSubstring("additionally failed to clean up remote temp file"))
		Expect(err.Error()).To(ContainSubstring("cleanup failed"))
	})

	It("keeps automatic sync best-effort while strict sync treats skips as errors", func() {
		stdout, _, _ := testhelper.SetupTestLogger()
		syncCalls := 0
		cleanupErr := errors.New("remove local standby history sync temp directory: cleanup failed")
		historyStandbySync = func() historyStandbySyncResult {
			syncCalls++
			return historyStandbySyncResult{err: cleanupErr}
		}

		result := syncHistoryStandbyBestEffort(false)
		Expect(errors.Is(result.err, cleanupErr)).To(BeTrue())
		Expect(syncCalls).To(Equal(1))
		Expect(string(stdout.Contents())).To(ContainSubstring("History db sync to standby coordinator failed; standby history may be stale: remove local standby history sync temp directory: cleanup failed"))

		err := syncHistoryStandbyStrict()
		Expect(errors.Is(err, cleanupErr)).To(BeTrue())

		historyStandbySync = func() historyStandbySyncResult {
			return historyStandbySyncResult{skipReason: "no up standby coordinator found"}
		}
		err = syncHistoryStandbyStrict()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("history db sync to standby coordinator skipped: no up standby coordinator found"))
	})

	It("skips disabled automatic sync without invoking discovery", func() {
		syncCalls := 0
		historyStandbySync = func() historyStandbySyncResult {
			syncCalls++
			return historyStandbySyncResult{err: errors.New("sync should not run")}
		}

		result := syncHistoryStandbyBestEffort(true)

		Expect(result.err).ToNot(HaveOccurred())
		Expect(result.skipReason).To(Equal("disabled by --" + noHistorySyncStandbyFlagName))
		Expect(syncCalls).To(Equal(0))
	})
})

func createHistoryStandbySyncSQLiteDB(path string) {
	Expect(os.MkdirAll(filepath.Dir(path), 0o700)).To(Succeed())
	db, err := sql.Open("sqlite3", historyStandbySyncSQLiteURI(path, "rwc"))
	Expect(err).ToNot(HaveOccurred())
	defer db.Close()

	_, err = db.Exec("CREATE TABLE sync_test (id INTEGER PRIMARY KEY, value TEXT)")
	Expect(err).ToNot(HaveOccurred())
	_, err = db.Exec("INSERT INTO sync_test (value) VALUES ('present')")
	Expect(err).ToNot(HaveOccurred())
}

func setupHistoryStandbySyncClusterConn(primaryDataDir, standbyDataDir string, standbyErr error, expectStandby bool) sqlmock.Sqlmock {
	return setupHistoryStandbySyncClusterConnWithHook(primaryDataDir, standbyDataDir, standbyErr, expectStandby, func(db *sqlx.DB) (*sqlx.DB, error) {
		return db, nil
	})
}

func setupHistoryStandbySyncClusterConnWithHook(
	primaryDataDir string,
	standbyDataDir string,
	standbyErr error,
	expectStandby bool,
	hook func(*sqlx.DB) (*sqlx.DB, error),
) sqlmock.Sqlmock {
	sqlDB, mock, err := sqlmock.New()
	Expect(err).ToNot(HaveOccurred())
	db := sqlx.NewDb(sqlDB, "sqlmock")
	mock.ExpectQuery(regexp.QuoteMeta(historyStandbySyncPrimarySQL)).
		WillReturnRows(sqlmock.NewRows([]string{"datadir"}).AddRow(primaryDataDir))
	if expectStandby {
		if standbyErr != nil {
			mock.ExpectQuery(regexp.QuoteMeta(historyStandbySyncStandbySQL)).WillReturnError(standbyErr)
		} else {
			mock.ExpectQuery(regexp.QuoteMeta(historyStandbySyncStandbySQL)).
				WillReturnRows(sqlmock.NewRows([]string{"hostname", "datadir"}).AddRow("sdw-standby", standbyDataDir))
		}
	}
	mock.ExpectClose()
	historyStandbySyncOpenClusterConn = func() (*sqlx.DB, error) {
		return hook(db)
	}
	return mock
}

func setHistoryStandbySyncCommands(responses []historyStandbySyncCommandResponse) *[]historyStandbySyncCommandCall {
	calls := make([]historyStandbySyncCommandCall, 0)
	execCombinedOutputCommand = func(name string, args ...string) combinedOutputCommand {
		calls = append(calls, historyStandbySyncCommandCall{name: name, args: append([]string{}, args...)})
		response := historyStandbySyncCommandResponse{}
		if len(calls) <= len(responses) {
			response = responses[len(calls)-1]
		}
		return historyStandbySyncFakeCommand{output: response.output, err: response.err}
	}
	return &calls
}
