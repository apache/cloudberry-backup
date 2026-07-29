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
	"bytes"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	stdpath "path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const upStandbyCoordinatorQuery = `
	SELECT hostname, datadir
	FROM gp_segment_configuration
	WHERE content = -1 AND role = 'm' AND status = 'u'`

type standbyCoordinatorTarget struct {
	Hostname string `db:"hostname"`
	DataDir  string `db:"datadir"`
}

type historyLogicalRow struct {
	Timestamp   string
	Status      string
	DateDeleted string
}

func discoverUpStandbyCoordinator() standbyCoordinatorTarget {
	var targets []standbyCoordinatorTarget
	err := backupConn.Select(&targets, upStandbyCoordinatorQuery)
	Expect(err).ToNot(HaveOccurred())
	if len(targets) == 0 {
		Skip("standby history sync requires an up standby coordinator")
	}
	Expect(targets).To(HaveLen(1), "expected exactly one up standby coordinator")
	return targets[0]
}

func quoteRemoteShellPath(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func copyStandbyHistoryDB(target standbyCoordinatorTarget) string {
	tempDir, err := os.MkdirTemp("", "gpbackup-history-standby-e2e-")
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	localPath := stdpath.Join(tempDir, "gpbackup_history.db")
	localFile, err := os.OpenFile(localPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	Expect(err).ToNot(HaveOccurred())

	remotePath := stdpath.Join(target.DataDir, "gpbackup_history.db")
	remoteCommand := fmt.Sprintf("cat -- %s", quoteRemoteShellPath(remotePath))
	command := exec.Command(
		"ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=30",
		target.Hostname,
		remoteCommand,
	)
	command.Stdout = localFile
	var stderr bytes.Buffer
	command.Stderr = &stderr

	runErr := command.Run()
	closeErr := localFile.Close()
	Expect(runErr).ToNot(HaveOccurred(), "copy %s:%s: %s", target.Hostname, remotePath, strings.TrimSpace(stderr.String()))
	Expect(closeErr).ToNot(HaveOccurred())
	return localPath
}

func readHistoryLogicalRows(historyDBPath string) []historyLogicalRow {
	dsn := (&url.URL{Scheme: "file", Path: historyDBPath}).String() + "?mode=ro"
	db, err := sql.Open("sqlite3", dsn)
	Expect(err).ToNot(HaveOccurred())
	defer db.Close()

	var quickCheck string
	err = db.QueryRow("PRAGMA quick_check").Scan(&quickCheck)
	Expect(err).ToNot(HaveOccurred())
	Expect(quickCheck).To(Equal("ok"))

	rows, err := db.Query(`
		SELECT timestamp, status, date_deleted
		FROM backups
		ORDER BY timestamp`)
	Expect(err).ToNot(HaveOccurred())
	defer rows.Close()

	logicalRows := make([]historyLogicalRow, 0)
	for rows.Next() {
		var row historyLogicalRow
		Expect(rows.Scan(&row.Timestamp, &row.Status, &row.DateDeleted)).To(Succeed())
		logicalRows = append(logicalRows, row)
	}
	Expect(rows.Err()).ToNot(HaveOccurred())
	return logicalRows
}

func findHistoryLogicalRow(rows []historyLogicalRow, timestamp string) historyLogicalRow {
	for _, row := range rows {
		if row.Timestamp == timestamp {
			return row
		}
	}
	Fail(fmt.Sprintf("history row %s was not found", timestamp))
	return historyLogicalRow{}
}

var _ = Describe("history database standby sync", func() {
	var (
		primaryHistoryDB string
		standbyTarget    standbyCoordinatorTarget
	)

	BeforeEach(func() {
		if useOldBackupVersion {
			Skip("standby history sync is not applicable in old backup version mode")
		}
		end_to_end_setup()
		standbyTarget = discoverUpStandbyCoordinator()
		primaryHistoryDB = getHistoryDBPathForCluster()
	})

	AfterEach(func() {
		end_to_end_teardown()
	})

	It("keeps standby history logically consistent across automatic, disabled, explicit, and mutation sync", func() {
		baselineOutput := gpbackup(
			gpbackupPath,
			backupHelperPath,
			"--backup-dir", backupDir,
		)
		baselineTimestamp := getBackupTimestamp(string(baselineOutput))
		Expect(baselineTimestamp).ToNot(BeEmpty())

		primaryBaseline := readHistoryLogicalRows(primaryHistoryDB)
		standbyBaseline := readHistoryLogicalRows(copyStandbyHistoryDB(standbyTarget))
		Expect(standbyBaseline).To(Equal(primaryBaseline))
		Expect(findHistoryLogicalRow(standbyBaseline, baselineTimestamp).Status).To(Equal("Success"))

		disabledOutput := gpbackup(
			gpbackupPath,
			backupHelperPath,
			"--backup-dir", backupDir,
			"--no-history-sync-standby",
		)
		disabledTimestamp := getBackupTimestamp(string(disabledOutput))
		Expect(disabledTimestamp).ToNot(BeEmpty())

		primaryAfterDisabledSync := readHistoryLogicalRows(primaryHistoryDB)
		Expect(findHistoryLogicalRow(primaryAfterDisabledSync, disabledTimestamp).Status).To(Equal("Success"))
		standbyAfterDisabledSync := readHistoryLogicalRows(copyStandbyHistoryDB(standbyTarget))
		Expect(standbyAfterDisabledSync).To(Equal(standbyBaseline))

		historySyncCommand := exec.Command(
			gpbackmanPath,
			"history-sync",
			"--auto-load-history-db",
		)
		historySyncCommand.Env = append(
			os.Environ(),
			fmt.Sprintf("COORDINATOR_DATA_DIRECTORY=%s", stdpath.Dir(primaryHistoryDB)),
		)
		mustRunCommand(historySyncCommand)

		primaryAfterExplicitSync := readHistoryLogicalRows(primaryHistoryDB)
		standbyAfterExplicitSync := readHistoryLogicalRows(copyStandbyHistoryDB(standbyTarget))
		Expect(standbyAfterExplicitSync).To(Equal(primaryAfterExplicitSync))
		Expect(findHistoryLogicalRow(standbyAfterExplicitSync, disabledTimestamp).Status).To(Equal("Success"))

		gpbackman(
			"backup-delete",
			"--history-db", primaryHistoryDB,
			"--timestamp", baselineTimestamp,
			"--backup-dir", backupDir,
		)

		primaryAfterDelete := readHistoryLogicalRows(primaryHistoryDB)
		deletedRow := findHistoryLogicalRow(primaryAfterDelete, baselineTimestamp)
		Expect(deletedRow.DateDeleted).ToNot(BeEmpty())
		Expect(deletedRow.DateDeleted).ToNot(Equal("In progress"))

		standbyAfterDelete := readHistoryLogicalRows(copyStandbyHistoryDB(standbyTarget))
		Expect(standbyAfterDelete).To(Equal(primaryAfterDelete))
		Expect(findHistoryLogicalRow(standbyAfterDelete, baselineTimestamp).DateDeleted).To(Equal(deletedRow.DateDeleted))
	})
})
