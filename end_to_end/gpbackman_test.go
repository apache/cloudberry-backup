package end_to_end_test

import (
	"fmt"
	"strings"

	"github.com/apache/cloudberry-backup/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// countBackupInfoLines counts the number of backup entry rows in backup-info
// output. Data rows contain '|' separators but do not contain the header
// label "TIMESTAMP".
func countBackupInfoLines(output []byte) int {
	count := 0
	for _, line := range strings.Split(string(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "|") &&
			!strings.Contains(trimmed, "TIMESTAMP") &&
			!isSeparatorLine(trimmed) {
			count++
		}
	}
	return count
}

// isSeparatorLine returns true for lines like "---+---+---".
func isSeparatorLine(line string) bool {
	for _, c := range line {
		if c != '-' && c != '+' && c != ' ' {
			return false
		}
	}
	return true
}

var _ = Describe("gpbackman end to end tests", func() {

	// ------------------------------------------------------------------ //
	//  backup-info
	// ------------------------------------------------------------------ //
	Describe("backup-info", func() {
		var (
			historyDB    string
			timestampMap map[string]string
		)

		BeforeEach(func() {
			end_to_end_setup()
			historyDB = getHistoryDBPathForCluster()
			timestampMap = make(map[string]string)

			// 1. Full local backup (with --leaf-partition-data for incremental compatibility)
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--leaf-partition-data")
			timestampMap["full_local"] = getBackupTimestamp(string(output))

			// 2. Full local backup with --include-table
			output = gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--include-table", "public.foo")
			timestampMap["full_include_table"] = getBackupTimestamp(string(output))

			// 3. Full local backup with --exclude-schema
			output = gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--exclude-schema", "schema2")
			timestampMap["full_exclude_schema"] = getBackupTimestamp(string(output))

			// 4. Incremental local backup (depends on full_local)
			output = gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--incremental",
				"--leaf-partition-data")
			timestampMap["incremental"] = getBackupTimestamp(string(output))

			// 5. Metadata-only backup
			output = gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--metadata-only")
			timestampMap["metadata_only"] = getBackupTimestamp(string(output))
		})

		AfterEach(func() {
			end_to_end_teardown()
		})

		It("lists all backups", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
			)
			lines := countBackupInfoLines(output)
			Expect(lines).To(BeNumerically(">=", 5),
				fmt.Sprintf("Expected at least 5 backup entries, got %d.\nOutput:\n%s",
					lines, string(output)))
		})

		It("filters by type full", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--type", "full",
			)
			Expect(string(output)).To(ContainSubstring("full"))
			Expect(string(output)).ToNot(ContainSubstring("incremental"))
		})

		It("filters by type incremental", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--type", "incremental",
			)
			Expect(string(output)).To(ContainSubstring("incremental"))
			lines := countBackupInfoLines(output)
			Expect(lines).To(BeNumerically(">=", 1),
				"Expected at least 1 incremental backup")
		})

		It("filters by type metadata-only", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--type", "metadata-only",
			)
			Expect(string(output)).To(ContainSubstring("metadata-only"))
			lines := countBackupInfoLines(output)
			Expect(lines).To(BeNumerically(">=", 1),
				"Expected at least 1 metadata-only backup")
		})

		It("filters by include-table", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--table", "public.foo",
			)
			Expect(string(output)).To(ContainSubstring(timestampMap["full_include_table"]))
		})

		It("filters by exclude-schema", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--schema", "schema2",
				"--exclude",
			)
			Expect(string(output)).To(ContainSubstring(timestampMap["full_exclude_schema"]))
		})

		It("shows backup chain with --timestamp", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--timestamp", timestampMap["full_local"],
			)
			outputStr := string(output)
			Expect(outputStr).To(ContainSubstring(timestampMap["full_local"]),
				"Expected the specified backup timestamp in the output")
		})

		It("shows detail with --timestamp --detail", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--timestamp", timestampMap["incremental"],
				"--detail",
			)
			Expect(string(output)).To(Or(
				ContainSubstring("OBJECT FILTERING"),
				ContainSubstring("object filtering"),
			))
		})

		It("shows detail for all backups with --detail", func() {
			output := gpbackman(
				"backup-info",
				"--history-db", historyDB,
				"--detail",
			)
			Expect(string(output)).To(Or(
				ContainSubstring("OBJECT FILTERING"),
				ContainSubstring("object filtering"),
			))
			Expect(string(output)).To(ContainSubstring("public.foo"))
		})

		It("rejects incompatible flags --timestamp with --type", func() {
			_, err := gpbackmanWithError(
				"backup-info",
				"--history-db", historyDB,
				"--timestamp", timestampMap["full_local"],
				"--type", "full",
			)
			Expect(err).To(HaveOccurred())
		})

		It("rejects invalid timestamp format", func() {
			_, err := gpbackmanWithError(
				"backup-info",
				"--history-db", historyDB,
				"--timestamp", "invalid",
			)
			Expect(err).To(HaveOccurred())
		})
	})

	// ------------------------------------------------------------------ //
	//  report-info
	// ------------------------------------------------------------------ //
	Describe("report-info", func() {
		var (
			historyDB    string
			timestampMap map[string]string
		)

		BeforeEach(func() {
			end_to_end_setup()
			historyDB = getHistoryDBPathForCluster()
			timestampMap = make(map[string]string)

			// 1. Full local backup
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestampMap["full_local"] = getBackupTimestamp(string(output))

			// 2. Plugin backup using example_plugin
			copyPluginToAllHosts(backupConn, examplePluginExec)
			output = gpbackup(gpbackupPath, backupHelperPath,
				"--plugin-config", examplePluginTestConfig)
			timestampMap["plugin"] = getBackupTimestamp(string(output))
		})

		AfterEach(func() {
			end_to_end_teardown()
		})

		It("displays local backup report with --backup-dir", func() {
			output := gpbackman(
				"report-info",
				"--history-db", historyDB,
				"--timestamp", timestampMap["full_local"],
				"--backup-dir", backupDir,
			)
			Expect(string(output)).To(ContainSubstring("Backup Report"))
			Expect(string(output)).To(ContainSubstring(timestampMap["full_local"]))
		})

		It("displays local backup report without --backup-dir", func() {
			output := gpbackman(
				"report-info",
				"--history-db", historyDB,
				"--timestamp", timestampMap["full_local"],
			)
			Expect(string(output)).To(ContainSubstring("Backup Report"))
		})

		It("displays plugin backup report", func() {
			ts := timestampMap["plugin"]
			reportDir := fmt.Sprintf("/tmp/plugin_dest/%s/%s", ts[:8], ts)
			output := gpbackman(
				"report-info",
				"--history-db", historyDB,
				"--timestamp", ts,
				"--plugin-config", examplePluginTestConfig,
				"--plugin-report-file-path", reportDir,
			)
			Expect(string(output)).To(ContainSubstring("Backup Report"))
			Expect(string(output)).To(ContainSubstring(ts))
		})

		It("rejects --plugin-report-file-path without --plugin-config", func() {
			_, err := gpbackmanWithError(
				"report-info",
				"--history-db", historyDB,
				"--timestamp", timestampMap["full_local"],
				"--plugin-report-file-path", "/tmp/fake_report",
			)
			Expect(err).To(HaveOccurred())
		})
	})

	// ------------------------------------------------------------------ //
	//  backup-delete
	// ------------------------------------------------------------------ //
	Describe("backup-delete", func() {
		var historyDB string

		BeforeEach(func() {
			end_to_end_setup()
			historyDB = getHistoryDBPathForCluster()
		})

		AfterEach(func() {
			end_to_end_teardown()
		})

		It("deletes a local backup by timestamp", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			dateDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(dateDeleted).ToNot(BeEmpty())
			Expect(dateDeleted).ToNot(Equal("In progress"))

			fpInfo := filepath.NewFilePathInfo(backupCluster, backupDir, timestamp, "", false)
			backupDirCoordinator := fpInfo.GetDirForContent(-1)
			Expect(backupDirCoordinator).ToNot(BeADirectory())
		})

		It("deletes a local backup without --backup-dir", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
			)

			dateDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(dateDeleted).ToNot(BeEmpty())
		})

		It("deletes with --cascade for incremental chain", func() {
			fullOutput := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--leaf-partition-data")
			fullTimestamp := getBackupTimestamp(string(fullOutput))

			incrOutput := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--incremental",
				"--leaf-partition-data")
			incrTimestamp := getBackupTimestamp(string(incrOutput))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", fullTimestamp,
				"--backup-dir", backupDir,
				"--cascade",
			)

			fullDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", fullTimestamp))
			Expect(fullDeleted).ToNot(BeEmpty())

			incrDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", incrTimestamp))
			Expect(incrDeleted).ToNot(BeEmpty())
		})

		It("deletes a plugin backup", func() {
			copyPluginToAllHosts(backupConn, examplePluginExec)

			output := gpbackup(gpbackupPath, backupHelperPath,
				"--plugin-config", examplePluginTestConfig)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--plugin-config", examplePluginTestConfig,
			)

			dateDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(dateDeleted).ToNot(BeEmpty())
			Expect(dateDeleted).ToNot(Equal("In progress"))
		})

		It("fails for non-existent timestamp", func() {
			_, err := gpbackmanWithError(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", "29991231235959",
				"--backup-dir", backupDir,
			)
			Expect(err).To(HaveOccurred())
		})

		It("skips already-deleted backup without --force", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			// Second delete without --force should succeed without error.
			// gpbackman silently skips already-deleted backups (logged at debug level).
			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			dateDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(dateDeleted).ToNot(BeEmpty(),
				"Backup should still be marked as deleted after second delete attempt")
		})

		It("re-deletes with --force", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
				"--force",
				"--ignore-errors",
			)

			dateDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(dateDeleted).ToNot(BeEmpty())
		})
	})

	// ------------------------------------------------------------------ //
	//  backup-delete: local file cleanup on segments
	// ------------------------------------------------------------------ //
	Describe("backup-delete local file cleanup", func() {
		var historyDB string

		BeforeEach(func() {
			end_to_end_setup()
			historyDB = getHistoryDBPathForCluster()
		})

		AfterEach(func() {
			end_to_end_teardown()
		})

		It("removes backup files after deletion", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--single-backup-dir")
			timestamp := getBackupTimestamp(string(output))

			fpInfo := filepath.NewFilePathInfo(backupCluster, backupDir, timestamp, "", true)
			backupTimestampDir := fpInfo.GetDirForContent(-1)
			Expect(backupTimestampDir).To(BeADirectory(),
				"Backup directory should exist before deletion")

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			Expect(backupTimestampDir).ToNot(BeADirectory(),
				"Backup directory should be removed after deletion")
		})
	})

	// ------------------------------------------------------------------ //
	//  backup-clean
	// ------------------------------------------------------------------ //
	Describe("backup-clean", func() {
		var historyDB string

		BeforeEach(func() {
			end_to_end_setup()
			historyDB = getHistoryDBPathForCluster()
		})

		AfterEach(func() {
			end_to_end_teardown()
		})

		It("cleans local backups with --before-timestamp", func() {
			output1 := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp1 := getBackupTimestamp(string(output1))

			output2 := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp2 := getBackupTimestamp(string(output2))

			gpbackman(
				"backup-clean",
				"--history-db", historyDB,
				"--before-timestamp", timestamp2,
				"--backup-dir", backupDir,
			)

			dateDeleted1 := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp1))
			Expect(dateDeleted1).ToNot(BeEmpty(),
				fmt.Sprintf("Expected backup %s to be deleted", timestamp1))
		})

		It("cleans local backups with --after-timestamp", func() {
			output1 := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp1 := getBackupTimestamp(string(output1))

			output2 := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp2 := getBackupTimestamp(string(output2))

			gpbackman(
				"backup-clean",
				"--history-db", historyDB,
				"--after-timestamp", timestamp1,
				"--backup-dir", backupDir,
			)

			dateDeleted2 := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp2))
			Expect(dateDeleted2).ToNot(BeEmpty(),
				fmt.Sprintf("Expected backup %s to be deleted", timestamp2))
		})

		It("cleans plugin backups with --before-timestamp", func() {
			copyPluginToAllHosts(backupConn, examplePluginExec)

			output := gpbackup(gpbackupPath, backupHelperPath,
				"--plugin-config", examplePluginTestConfig)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-clean",
				"--history-db", historyDB,
				"--before-timestamp", timestamp,
				"--plugin-config", examplePluginTestConfig,
			)

			countStr := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT count(*) FROM backups WHERE timestamp = '%s' AND date_deleted = ''",
					timestamp))
			Expect(countStr).ToNot(BeEmpty())
		})

		It("cleans local backups with --cascade for incremental chain", func() {
			fullOutput := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--leaf-partition-data")
			fullTimestamp := getBackupTimestamp(string(fullOutput))

			_ = gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir,
				"--incremental",
				"--leaf-partition-data")

			gpbackman(
				"backup-clean",
				"--history-db", historyDB,
				"--before-timestamp", fullTimestamp,
				"--backup-dir", backupDir,
				"--cascade",
			)
			// Success if no error was thrown
		})
	})

	// ------------------------------------------------------------------ //
	//  history-clean
	// ------------------------------------------------------------------ //
	Describe("history-clean", func() {
		var historyDB string

		BeforeEach(func() {
			end_to_end_setup()
			historyDB = getHistoryDBPathForCluster()
		})

		AfterEach(func() {
			end_to_end_teardown()
		})

		It("cleans deleted backup entries from history DB", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			dateDeleted := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT date_deleted FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(dateDeleted).ToNot(BeEmpty())

			// --before-timestamp uses strictly less than (<), so use a
			// far-future cutoff to include the target timestamp.
			gpbackman(
				"history-clean",
				"--history-db", historyDB,
				"--before-timestamp", "99991231235959",
			)

			countStr := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT count(*) FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(countStr).To(Equal("0"),
				fmt.Sprintf("Expected backup %s to be removed from history DB", timestamp))
		})

		It("cleans with --older-than-days 0", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp := getBackupTimestamp(string(output))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp,
				"--backup-dir", backupDir,
			)

			gpbackman(
				"history-clean",
				"--history-db", historyDB,
				"--older-than-days", "0",
			)

			countStr := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT count(*) FROM backups WHERE timestamp = '%s'", timestamp))
			Expect(countStr).To(Equal("0"))
		})

		It("leaves non-deleted entries intact", func() {
			output1 := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp1 := getBackupTimestamp(string(output1))

			output2 := gpbackup(gpbackupPath, backupHelperPath,
				"--backup-dir", backupDir)
			timestamp2 := getBackupTimestamp(string(output2))

			gpbackman(
				"backup-delete",
				"--history-db", historyDB,
				"--timestamp", timestamp1,
				"--backup-dir", backupDir,
			)

			gpbackman(
				"history-clean",
				"--history-db", historyDB,
				"--older-than-days", "0",
			)

			count1 := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT count(*) FROM backups WHERE timestamp = '%s'", timestamp1))
			Expect(count1).To(Equal("0"),
				"Deleted backup should be removed from history")

			count2 := queryHistoryDB(historyDB,
				fmt.Sprintf("SELECT count(*) FROM backups WHERE timestamp = '%s'", timestamp2))
			Expect(count2).To(Equal("1"),
				"Non-deleted backup should remain in history")
		})
	})

	// ------------------------------------------------------------------ //
	//  version & help
	// ------------------------------------------------------------------ //
	Describe("gpbackman --version", func() {
		It("prints version information", func() {
			output := gpbackman("--version")
			Expect(string(output)).To(ContainSubstring("gpbackman"))
		})
	})

	Describe("gpbackman --help", func() {
		It("prints help for all subcommands", func() {
			for _, subcmd := range []string{
				"backup-info", "backup-delete", "backup-clean",
				"history-clean", "report-info",
			} {
				output := gpbackman(subcmd, "--help")
				Expect(string(output)).To(ContainSubstring(subcmd),
					fmt.Sprintf("Help for %s should mention the command name", subcmd))
			}
		})
	})
})
