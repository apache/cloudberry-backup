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

package end_to_end_test

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-go-libs/dbconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
)

// Wait for this backup's lock request, never a fixed sleep or another client's lock.
func waitForSnapshotBackupLock(relation string) {
	query := fmt.Sprintf(`SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
  WHERE l.relation='%s'::regclass AND NOT l.granted AND l.mode='AccessShareLock'
  AND a.datname=current_database() AND a.application_name LIKE 'gpbackup_%%' AND a.query LIKE 'LOCK TABLE%%'`, relation)
	Eventually(func() (int, error) { var count int; err := backupConn.Get(&count, query); return count, err }, 30*time.Second, 100*time.Millisecond).Should(BeNumerically(">", 0))
}

func startSnapshotBackup(args ...string) *Session {
	args = append([]string{"--dbname", "testdb", "--backup-dir", backupDir, "--verbose", "--no-history-sync-standby"}, args...)
	session, err := Start(exec.Command(gpbackupPath, args...), GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		if session.ExitCode() == -1 {
			session.Terminate()
			Eventually(session, 30*time.Second).Should(Exit())
		}
	})
	return session
}

func snapshotRetryRows(conn *dbconn.DBConn) []int {
	var rows []int
	Expect(conn.Select(&rows, "SELECT id FROM snapshot_retry.t ORDER BY id")).To(Succeed())
	return rows
}

// Server backend removal from pg_stat_activity can lag the client process exit.
func waitForSnapshotBackupSessionsToClose() {
	Eventually(func() (int, error) {
		var count int
		err := backupConn.Get(&count, "SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name LIKE 'gpbackup_%'")
		return count, err
	}, 10*time.Second, 100*time.Millisecond).Should(BeZero())
}

var _ = Describe("snapshot lock retry", func() {
	BeforeEach(func() {
		if useOldBackupVersion || !backupConn.Version.IsCBDB() {
			Skip("snapshot retries require Cloudberry and current gpbackup")
		}
		end_to_end_setup()
		backupConn.MustExec("CREATE SCHEMA snapshot_retry")
	})
	AfterEach(func() {
		backupConn.MustExec("DROP SCHEMA IF EXISTS snapshot_retry CASCADE")
		restoreConn.MustExec("DROP SCHEMA IF EXISTS snapshot_retry CASCADE")
		end_to_end_teardown()
	})

	createTable := func(am string, partitioned bool) {
		if partitioned {
			backupConn.MustExec(fmt.Sprintf(`CREATE TABLE snapshot_retry.t(id int) USING %s DISTRIBUTED BY(id) PARTITION BY RANGE(id);
    CREATE TABLE snapshot_retry.middle PARTITION OF snapshot_retry.t FOR VALUES FROM(0) TO(10) PARTITION BY RANGE(id);
    CREATE TABLE snapshot_retry.leaf PARTITION OF snapshot_retry.middle FOR VALUES FROM(0) TO(10) USING %s`, am, am))
		} else {
			backupConn.MustExec(fmt.Sprintf("CREATE TABLE snapshot_retry.t(id int) USING %s DISTRIBUTED BY(id)", am))
		}
		backupConn.MustExec("INSERT INTO snapshot_retry.t VALUES(1),(2),(3)")
	}
	truncateDuringBackup := func(partitioned bool, args ...string) *Session {
		writer := testutils.SetupTestDbConn("testdb")
		defer writer.Close()
		writer.MustBegin()
		defer func() {
			if writer.Tx[0] != nil {
				writer.MustRollback()
			}
		}()
		relation := "snapshot_retry.t"
		if partitioned {
			relation = "snapshot_retry.leaf"
		}
		writer.MustExec("TRUNCATE " + relation)
		writer.MustExec("INSERT INTO " + relation + " VALUES(4),(5),(6)")
		session := startSnapshotBackup(args...)
		waitForSnapshotBackupLock(relation)
		writer.MustCommit()
		Eventually(session, 90*time.Second).Should(Exit())
		return session
	}
	restore := func(session *Session) {
		Expect(session.ExitCode()).To(Equal(0))
		Expect(string(session.Out.Contents())).To(ContainSubstring("Starting snapshot attempt 2 of 3"))
		ts := getBackupTimestamp(string(session.Out.Contents()))
		gprestore(gprestorePath, restoreHelperPath, ts, "--backup-dir", backupDir, "--redirect-db", "restoredb")
		Expect(snapshotRetryRows(backupConn)).To(Equal([]int{4, 5, 6}))
		Expect(snapshotRetryRows(restoreConn)).To(Equal([]int{4, 5, 6}))
	}
	for _, am := range []string{"heap", "ao_row", "ao_column", "pax"} {
		for _, shape := range []string{"plain", "nested root data", "nested leaf data"} {
			am, shape := am, shape
			It(fmt.Sprintf("restores concurrent TRUNCATE for %s %s", am, shape), func() {
				if am == "pax" && dbconn.MustSelectString(backupConn, "SELECT count(*) AS string FROM pg_am WHERE amname='pax'") == "0" {
					Skip("PAX unavailable")
				}
				partitioned := shape != "plain"
				createTable(am, partitioned)
				args := []string{"--include-schema", "snapshot_retry", "--jobs", "2"}
				if shape == "nested leaf data" {
					args = append(args, "--leaf-partition-data")
				}
				restore(truncateDuringBackup(partitioned, args...))
			})
		}
	}
	It("retries with single data file and a table filter file", func() {
		createTable("heap", true)
		file, err := os.CreateTemp("", "snapshot-include")
		Expect(err).NotTo(HaveOccurred())
		defer os.Remove(file.Name())
		_, err = file.WriteString("snapshot_retry.t\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(file.Close()).To(Succeed())
		restoreConn.MustExec("CREATE SCHEMA snapshot_retry")
		restore(truncateDuringBackup(true, "--include-table-file", file.Name(), "--single-data-file", "--leaf-partition-data"))
	})
	It("fails the entire backup when only one attempt is allowed", func() {
		createTable("heap", false)
		session := truncateDuringBackup(false, "--include-schema", "snapshot_retry", "--snapshot-attempts", "1")
		Expect(session.ExitCode()).NotTo(Equal(0))
		Expect(string(session.Out.Contents())).To(ContainSubstring("after 1 attempt(s): snapshot_retry.t"))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Backup completed successfully"))
	})
	It("fails after two consecutive storage conflicts", func() {
		createTable("heap", false)
		first := testutils.SetupTestDbConn("testdb")
		defer first.Close()
		first.MustBegin()
		defer func() {
			if first.Tx[0] != nil {
				first.MustRollback()
			}
		}()
		first.MustExec("TRUNCATE snapshot_retry.t; INSERT INTO snapshot_retry.t VALUES(4),(5),(6)")
		session := startSnapshotBackup("--include-schema", "snapshot_retry", "--snapshot-attempts", "2")
		waitForSnapshotBackupLock("snapshot_retry.t")
		second := testutils.SetupTestDbConn("testdb")
		defer second.Close()
		var pid int
		Expect(second.Get(&pid, "SELECT pg_backend_pid()")).To(Succeed())
		second.MustBegin()
		defer func() {
			if second.Tx[0] != nil {
				second.MustRollback()
			}
		}()
		defer func() {
			if first.Tx[0] != nil {
				first.MustRollback()
			}
		}()
		ready := make(chan error, 1)
		go func() {
			_, err := second.Exec("TRUNCATE snapshot_retry.t")
			if err == nil {
				_, err = second.Exec("INSERT INTO snapshot_retry.t VALUES(7),(8),(9)")
			}
			ready <- err
		}()
		Eventually(func() (int, error) {
			var count int
			err := backupConn.Get(&count, fmt.Sprintf("SELECT count(*) FROM pg_locks WHERE pid=%d AND NOT granted AND relation='snapshot_retry.t'::regclass", pid))
			return count, err
		}, 30*time.Second, 100*time.Millisecond).Should(BeNumerically(">", 0))
		first.MustCommit()
		Eventually(ready, 30*time.Second).Should(Receive(BeNil()))
		waitForSnapshotBackupLock("snapshot_retry.t")
		second.MustCommit()
		Eventually(session, 90*time.Second).Should(Exit())
		Expect(session.ExitCode()).NotTo(Equal(0))
		Expect(string(session.Out.Contents())).To(ContainSubstring("after 2 attempt(s): snapshot_retry.t"))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Backup completed successfully"))
		waitForSnapshotBackupSessionsToClose()
	})

	It("restores full then incremental after a retry with heap file hashes", func() {
		createTable("heap", false)
		full := gpbackup(gpbackupPath, backupHelperPath, "--backup-dir", backupDir, "--include-schema", "snapshot_retry", "--leaf-partition-data", "--heap-file-hash")
		fullTS := getBackupTimestamp(string(full))
		session := truncateDuringBackup(false, "--include-schema", "snapshot_retry", "--leaf-partition-data", "--heap-file-hash", "--incremental", "--from-timestamp", fullTS)
		Expect(session.ExitCode()).To(Equal(0))
		Expect(string(session.Out.Contents())).To(ContainSubstring("Snapshot retry disables heap file hashes"))
		ts := getBackupTimestamp(string(session.Out.Contents()))
		gprestore(gprestorePath, restoreHelperPath, ts, "--backup-dir", backupDir, "--redirect-db", "restoredb")
		Expect(snapshotRetryRows(restoreConn)).To(Equal([]int{4, 5, 6}))
	})
	It("cancels blocked locks and leaves no backup sessions after interruption", func() {
		createTable("heap", false)
		writer := testutils.SetupTestDbConn("testdb")
		defer writer.Close()
		writer.MustBegin()
		defer writer.MustRollback()
		writer.MustExec("TRUNCATE snapshot_retry.t")
		session := startSnapshotBackup("--include-schema", "snapshot_retry")
		waitForSnapshotBackupLock("snapshot_retry.t")
		session.Signal(syscall.SIGINT)
		Eventually(session, 30*time.Second).Should(Exit())
		Expect(session.ExitCode()).NotTo(Equal(0))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Backup completed successfully"))
		waitForSnapshotBackupSessionsToClose()
	})
})
