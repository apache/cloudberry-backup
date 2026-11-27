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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-go-libs/cluster"
	"github.com/apache/cloudberry-go-libs/dbconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gexec"
	"gopkg.in/yaml.v2"
)

// Wait for this backup's lock request, never a fixed sleep or another client's lock.
func waitForSnapshotBackupLock(relation string) {
	query := fmt.Sprintf(`SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
  WHERE l.relation='%s'::regclass AND NOT l.granted AND l.mode='AccessShareLock'
  AND a.datname=current_database() AND a.application_name LIKE 'gpbackup_%%' AND a.query LIKE 'LOCK TABLE%%'`, relation)
	Eventually(func() (int, error) { var count int; err := backupConn.Get(&count, query); return count, err }, 30*time.Second, 100*time.Millisecond).Should(BeNumerically(">", 0))
}

func startSnapshotBackup(args ...string) *Session {
	if !hasCommandArgument(args, "--plugin-config") {
		args = append(args, "--backup-dir", backupDir)
	}
	args = append([]string{"--dbname", "testdb", "--verbose", "--no-history-sync-standby", "--snapshot-attempts", "1"}, args...)
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

var _ = Describe("snapshot lock preparation", func() {
	BeforeEach(func() {
		if useOldBackupVersion || !backupConn.Version.IsCBDB() {
			Skip("snapshot preparation requires Cloudberry and current gpbackup")
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
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Starting snapshot attempt"))
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
	It("restores with single data file and a table filter file", func() {
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
	DescribeTable("preserves the root copy when ATTACH starts after the backup snapshot", func(nested bool) {
		backupConn.MustExec("CREATE TABLE snapshot_retry.t(id int) DISTRIBUTED BY(id) PARTITION BY RANGE(id)")
		parent := "snapshot_retry.t"
		if nested {
			backupConn.MustExec(`CREATE TABLE snapshot_retry.middle PARTITION OF snapshot_retry.t
				FOR VALUES FROM (0) TO (100) PARTITION BY RANGE(id)`)
			parent = "snapshot_retry.middle"
		}
		backupConn.MustExec(fmt.Sprintf(`CREATE TABLE snapshot_retry.leaf PARTITION OF %s FOR VALUES FROM (0) TO (10);
			INSERT INTO snapshot_retry.t VALUES (1),(2),(3);
			CREATE TABLE snapshot_retry.new_leaf(id int) DISTRIBUTED BY(id);
			INSERT INTO snapshot_retry.new_leaf VALUES (11),(12),(13)`, parent))

		pluginPath := filepath.Join(backupDir, "snapshot-plugin.bash")
		pluginConfig := filepath.Join(backupDir, "snapshot-plugin.yaml")
		gate, release := filepath.Join(backupDir, "snapshot-ready"), filepath.Join(backupDir, "snapshot-release")
		plugin, err := os.ReadFile(examplePluginExec)
		Expect(err).NotTo(HaveOccurred())
		gatedSetup := fmt.Sprintf(`setup_plugin_for_backup(){
  if [[ "$3" == "coordinator" || "$3" == "master" ]]; then
    touch %q
    deadline=$((SECONDS + 45))
    until [[ -f %q ]]; do
      ((SECONDS < deadline)) || exit 1
      sleep 0.1
    done
  fi`, gate, release)
		Expect(os.WriteFile(pluginPath, []byte(strings.Replace(string(plugin), "setup_plugin_for_backup(){", gatedSetup, 1)), 0755)).To(Succeed())
		Expect(os.WriteFile(pluginConfig, []byte(fmt.Sprintf("executablepath: %s\noptions:\n  password: unknown\n", pluginPath)), 0600)).To(Succeed())
		copyPluginToAllHosts(backupConn, pluginPath)
		restoreConn.MustExec("CREATE SCHEMA snapshot_retry")

		writer := testutils.SetupTestDbConn("testdb")
		defer writer.Close()
		writer.MustExec("SET statement_timeout = '60s'")
		var pid int
		Expect(writer.Get(&pid, "SELECT pg_backend_pid()")).To(Succeed())
		session := startSnapshotBackup("--include-table", "snapshot_retry.t", "--plugin-config", pluginConfig)
		defer func() { _ = os.WriteFile(release, nil, 0600) }()
		Eventually(gate, 30*time.Second).Should(BeAnExistingFile())
		attachDone := make(chan error, 1)
		go func() {
			_, err := writer.Exec(fmt.Sprintf("ALTER TABLE %s ATTACH PARTITION snapshot_retry.new_leaf FOR VALUES FROM (10) TO (20)", parent))
			attachDone <- err
		}()
		Eventually(func() (bool, error) {
			select {
			case err := <-attachDone:
				return false, StopTrying(fmt.Sprintf("ATTACH finished before backup released the parent lock: %v", err))
			default:
			}
			var waiting bool
			query := fmt.Sprintf(`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE pid=%d
				AND relation='%s'::regclass AND mode='ShareUpdateExclusiveLock' AND NOT granted)`, pid, parent)
			err := backupConn.Get(&waiting, query)
			return waiting, err
		}, 10*time.Second, 50*time.Millisecond).Should(BeTrue())
		Expect(os.WriteFile(release, nil, 0600)).To(Succeed())
		Eventually(session, 90*time.Second).Should(Exit(0))
		Eventually(attachDone, 30*time.Second).Should(Receive(BeNil()))

		ts := getBackupTimestamp(string(session.Out.Contents()))
		gprestore(gprestorePath, restoreHelperPath, ts, "--redirect-db", "restoredb", "--plugin-config", pluginConfig)
		Expect(snapshotRetryRows(restoreConn)).To(Equal([]int{1, 2, 3}))
		Expect(snapshotRetryRows(backupConn)).To(Equal([]int{1, 2, 3, 11, 12, 13}))
	}, Entry("on the root", false), Entry("on a subparent", true))
	It("does not deadlock when another exclusive lock queues behind the backup", func() {
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
		session := startSnapshotBackup("--include-schema", "snapshot_retry", "--jobs", "2")
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
		Eventually(session, 90*time.Second).Should(Exit(0))
		Eventually(ready, 30*time.Second).Should(Receive(BeNil()))
		second.MustCommit()
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Starting snapshot attempt"))
		ts := getBackupTimestamp(string(session.Out.Contents()))
		gprestore(gprestorePath, restoreHelperPath, ts, "--backup-dir", backupDir, "--redirect-db", "restoredb")
		Expect(snapshotRetryRows(restoreConn)).To(Equal([]int{4, 5, 6}))
		Expect(snapshotRetryRows(backupConn)).To(Equal([]int{7, 8, 9}))
		waitForSnapshotBackupSessionsToClose()
	})

	It("restores an incremental after TRUNCATE and retains hashes for unchanged heap tables", func() {
		createTable("heap", false)
		backupConn.MustExec("CREATE TABLE snapshot_retry.unchanged(id int) DISTRIBUTED BY(id); INSERT INTO snapshot_retry.unchanged VALUES(42)")
		var count int
		Expect(backupConn.Get(&count, "SELECT count(*) FROM snapshot_retry.unchanged")).To(Succeed())
		full := gpbackup(gpbackupPath, backupHelperPath, "--backup-dir", backupDir, "--include-schema", "snapshot_retry", "--leaf-partition-data", "--heap-file-hash")
		fullTS := getBackupTimestamp(string(full))
		session := truncateDuringBackup(false, "--include-schema", "snapshot_retry", "--leaf-partition-data", "--heap-file-hash", "--incremental", "--from-timestamp", fullTS)
		Expect(session.ExitCode()).To(Equal(0))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Starting snapshot attempt"))
		var currentTOC toc.TOC
		ts := getBackupTimestamp(string(session.Out.Contents()))
		Expect(yaml.Unmarshal(getMetdataFileContents(backupDir, ts, "toc.yaml"), &currentTOC)).To(Succeed())
		Expect(currentTOC.IncrementalMetadata.Heap["snapshot_retry.unchanged"].FileHashMD5).NotTo(BeEmpty())
		gprestore(gprestorePath, restoreHelperPath, ts, "--backup-dir", backupDir, "--redirect-db", "restoredb")
		Expect(snapshotRetryRows(restoreConn)).To(Equal([]int{4, 5, 6}))
		next := gpbackup(gpbackupPath, backupHelperPath, "--backup-dir", backupDir, "--include-schema", "snapshot_retry", "--leaf-partition-data", "--heap-file-hash", "--incremental", "--from-timestamp", ts)
		nextTS := getBackupTimestamp(string(next))
		var nextTOC toc.TOC
		Expect(yaml.Unmarshal(getMetdataFileContents(backupDir, nextTS, "toc.yaml"), &nextTOC)).To(Succeed())
		Expect(nextTOC.IncrementalMetadata.Heap["snapshot_retry.unchanged"].FileHashMD5).NotTo(BeEmpty())
		for _, entry := range nextTOC.DataEntries {
			Expect(entry.Schema + "." + entry.Name).NotTo(Equal("snapshot_retry.unchanged"))
		}
	})
	DescribeTable("accepts a safe heap rewrite before the snapshot without retrying", func(command string) {
		createTable("heap", false)
		backupConn.MustExec("CREATE TABLE snapshot_retry.rewrite(id int) DISTRIBUTED BY(id); INSERT INTO snapshot_retry.rewrite VALUES(41),(42); CREATE INDEX snapshot_rewrite_idx ON snapshot_retry.rewrite(id)")
		if command == "ALTER TABLE snapshot_retry.rewrite SET TABLESPACE snapshot_rewrite_ts" {
			output := backupCluster.GenerateAndExecuteCommand("Creating snapshot test tablespace directories", cluster.ON_HOSTS|cluster.INCLUDE_COORDINATOR, func(host string) string { return "mkdir -p /tmp/snapshot_rewrite_ts" })
			Expect(output.NumErrors).To(BeZero())
			backupConn.MustExec("CREATE TABLESPACE snapshot_rewrite_ts LOCATION '/tmp/snapshot_rewrite_ts'")
			DeferCleanup(func() {
				backupConn.MustExec("DROP TABLESPACE snapshot_rewrite_ts")
				output := backupCluster.GenerateAndExecuteCommand("Removing snapshot test tablespace directories", cluster.ON_HOSTS|cluster.INCLUDE_COORDINATOR, func(host string) string { return "rmdir /tmp/snapshot_rewrite_ts" })
				Expect(output.NumErrors).To(BeZero())
			})
		}
		var before, after uint32
		Expect(backupConn.Get(&before, "SELECT relfilenode FROM pg_class WHERE oid='snapshot_retry.rewrite'::regclass")).To(Succeed())
		gate := testutils.SetupTestDbConn("testdb")
		defer gate.Close()
		gate.MustBegin()
		defer func() {
			if gate.Tx[0] != nil {
				gate.MustRollback()
			}
		}()
		gate.MustExec("LOCK TABLE snapshot_retry.t IN ACCESS EXCLUSIVE MODE")
		session := startSnapshotBackup("--include-schema", "snapshot_retry")
		waitForSnapshotBackupLock("snapshot_retry.t")
		backupConn.MustExec(command)
		Expect(backupConn.Get(&after, "SELECT relfilenode FROM pg_class WHERE oid='snapshot_retry.rewrite'::regclass")).To(Succeed())
		Expect(after).NotTo(Equal(before))
		gate.MustCommit()
		Eventually(session, 90*time.Second).Should(Exit(0))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Starting snapshot attempt"))
		ts := getBackupTimestamp(string(session.Out.Contents()))
		gprestore(gprestorePath, restoreHelperPath, ts, "--backup-dir", backupDir, "--redirect-db", "restoredb")
		var rows []int
		Expect(restoreConn.Select(&rows, "SELECT id FROM snapshot_retry.rewrite ORDER BY id")).To(Succeed())
		Expect(rows).To(Equal([]int{41, 42}))
	}, Entry("VACUUM FULL", "VACUUM FULL snapshot_retry.rewrite"),
		Entry("CLUSTER", "CLUSTER snapshot_retry.rewrite USING snapshot_rewrite_idx"),
		Entry("SET TABLESPACE", "ALTER TABLE snapshot_retry.rewrite SET TABLESPACE snapshot_rewrite_ts"))

	DescribeTable("cancels blocked locks and completes cleanup after interruption", func(args []string) {
		createTable("heap", false)
		writer := testutils.SetupTestDbConn("testdb")
		defer writer.Close()
		writer.MustBegin()
		defer writer.MustRollback()
		writer.MustExec("TRUNCATE snapshot_retry.t")
		session := startSnapshotBackup(append([]string{"--include-schema", "snapshot_retry"}, args...)...)
		waitForSnapshotBackupLock("snapshot_retry.t")
		var application string
		Expect(backupConn.Get(&application, "SELECT application_name FROM pg_stat_activity WHERE datname=current_database() AND application_name LIKE 'gpbackup_%' AND query LIKE 'LOCK TABLE%'")).To(Succeed())
		session.Signal(syscall.SIGINT)
		Eventually(session, 30*time.Second).Should(Exit())
		Expect(session.ExitCode()).NotTo(Equal(0))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Backup completed successfully"))
		Expect(string(session.Out.Contents())).NotTo(ContainSubstring("Encountered error during cleanup"))
		Expect(string(session.Out.Contents())).To(ContainSubstring("Cleanup complete"))
		_, err := os.Stat("/tmp/" + strings.TrimPrefix(application, "gpbackup_") + ".lck")
		Expect(os.IsNotExist(err)).To(BeTrue())
		waitForSnapshotBackupSessionsToClose()
	}, Entry("with history", []string{}), Entry("with single data file", []string{"--single-data-file"}), Entry("without history", []string{"--no-history"}))
})
