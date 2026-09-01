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

package gpbckpconfig

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apache/cloudberry-backup/history"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("utils_db tests", func() {
	Describe("OpenHistoryDB", func() {
		It("returns a friendly error and does not create a file when the path does not exist", func() {
			tempDir := GinkgoT().TempDir()
			missing := filepath.Join(tempDir, "does-not-exist.db")

			db, err := OpenHistoryDB(missing)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
			Expect(err.Error()).To(ContainSubstring("--history-db"))
			Expect(db).To(BeNil())

			// Critical regression: no empty SQLite file must have been created.
			_, statErr := os.Stat(missing)
			Expect(os.IsNotExist(statErr)).To(BeTrue(), "OpenHistoryDB must not create the file when it is missing")
		})

		It("opens an existing SQLite history database successfully", func() {
			tempDir := GinkgoT().TempDir()
			path := filepath.Join(tempDir, "gpbackup_history.db")

			// Seed an existing (but empty) SQLite file via the rwc URI mode.
			seed, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
			Expect(err).NotTo(HaveOccurred())
			Expect(seed.Ping()).To(Succeed())
			Expect(seed.Close()).To(Succeed())

			db, err := OpenHistoryDB(path)
			Expect(err).NotTo(HaveOccurred())
			Expect(db).NotTo(BeNil())
			Expect(db.Ping()).To(Succeed())
			Expect(db.Close()).To(Succeed())
		})
	})

	Describe("getBackupNameQuery", func() {
		It("returns correct query for various flag combinations", func() {
			tests := []struct {
				name  string
				showD bool
				showF bool
				want  string
			}{
				{
					name:  "show all",
					showD: true,
					showF: true,
					want:  `SELECT timestamp FROM backups ORDER BY timestamp DESC;`,
				},
				{
					name:  "show deleted",
					showD: true,
					showF: false,
					want:  `SELECT timestamp FROM backups WHERE status != 'Failure' ORDER BY timestamp DESC;`,
				},
				{
					name:  "show failed",
					showD: false,
					showF: true,
					want:  `SELECT timestamp FROM backups WHERE date_deleted IN ('', 'In progress', 'Plugin Backup Delete Failed', 'Local Delete Failed') ORDER BY timestamp DESC;`,
				},
				{
					name:  "show default",
					showD: false,
					showF: false,
					want:  `SELECT timestamp FROM backups WHERE status != 'Failure' AND date_deleted IN ('', 'In progress', 'Plugin Backup Delete Failed', 'Local Delete Failed') ORDER BY timestamp DESC;`,
				},
			}
			for _, tt := range tests {
				Expect(getBackupNameQuery(tt.showD, tt.showF)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("getBackupDependenciesQuery", func() {
		It("returns correct query", func() {
			want := `
SELECT timestamp 
FROM restore_plans
WHERE timestamp != 'TestBackup'
	AND restore_plan_timestamp = 'TestBackup'
ORDER BY timestamp DESC;
`
			Expect(getBackupDependenciesQuery("TestBackup")).To(Equal(want))
		})
	})

	Describe("getBackupNameBeforeTimestampQuery", func() {
		It("returns correct query", func() {
			want := fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp < '20240101120000' 
	AND status != '%s' 
	AND date_deleted IN ('', 'Plugin Backup Delete Failed', 'Local Delete Failed') 
ORDER BY timestamp DESC;
`, history.BackupStatusInProgress)
			Expect(getBackupNameBeforeTimestampQuery("20240101120000", "")).To(Equal(want))
		})
	})

	Describe("getBackupNameAfterTimestampQuery", func() {
		It("returns correct query", func() {
			want := fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp > '20240101120000' 
	AND status != '%s' 
	AND date_deleted IN ('', 'Plugin Backup Delete Failed', 'Local Delete Failed') 
ORDER BY timestamp DESC;
`, history.BackupStatusInProgress)
			Expect(getBackupNameAfterTimestampQuery("20240101120000", "")).To(Equal(want))
		})
	})

	Describe("getBackupNameForCleanBeforeTimestampQuery", func() {
		It("returns correct query", func() {
			want := `
SELECT timestamp 
FROM backups 
WHERE timestamp < '20240101120000' 
	AND date_deleted NOT IN ('', 'Plugin Backup Delete Failed', 'Local Delete Failed', 'In progress') 
ORDER BY timestamp DESC;
`
			Expect(getBackupNameForCleanBeforeTimestampQuery("20240101120000", "")).To(Equal(want))
		})
	})

	Describe("database-filtered backup name queries", func() {
		var historyDB *sql.DB

		BeforeEach(func() {
			var err error
			historyDB, err = sql.Open("sqlite3", "file:"+filepath.Join(GinkgoT().TempDir(), "history.db")+"?mode=rwc")
			Expect(err).NotTo(HaveOccurred())
			_, err = historyDB.Exec(`CREATE TABLE backups (timestamp TEXT, database_name TEXT, status TEXT, date_deleted TEXT)`)
			Expect(err).NotTo(HaveOccurred())
			for _, backup := range [][]string{
				{"20240101110000", "customer", "Success", ""},
				{"20240101100000", "Customer", "Success", ""},
				{"20240101090000", `"quoted db"`, "Success", ""},
				{"20240101080000", "customer's db", "Success", ""},
				{"20240102110000", "customer", "Success", ""},
				{"20240102100000", "Customer", "Success", ""},
				{"20240102090000", `"quoted db"`, "Success", ""},
				{"20240102080000", "customer's db", "Success", ""},
				{"20240101110000-clean", "customer", "Success", "20240103000000"},
				{"20240101100000-clean", "Customer", "Success", "20240103000000"},
				{"20240101090000-clean", `"quoted db"`, "Success", "20240103000000"},
				{"20240101080000-clean", "customer's db", "Success", "20240103000000"},
			} {
				_, err = historyDB.Exec(`INSERT INTO backups (timestamp, database_name, status, date_deleted) VALUES (?, ?, ?, ?)`, backup[0], backup[1], backup[2], backup[3])
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			Expect(historyDB.Close()).To(Succeed())
		})

		It("adds a bound database predicate only when a database is supplied", func() {
			unfiltered := getBackupNameBeforeTimestampQuery("20240101120000", "")
			filtered := getBackupNameBeforeTimestampQuery("20240101120000", "customer's db")
			Expect(unfiltered).NotTo(ContainSubstring("database_name"))
			Expect(filtered).To(ContainSubstring("AND database_name = ?"))
			Expect(filtered).NotTo(ContainSubstring("customer's db"))
		})

		It("returns a scan error from a backup-name query", func() {
			_, err := execQueryFunc("SELECT NULL", historyDB)
			Expect(err).To(HaveOccurred())
		})

		DescribeTable("returns only exact database matches while retaining unfiltered selection",
			func(query func(string, string, *sql.DB) ([]string, error), timestamp string, expectedAll []string, exact, quoted, apostrophe string) {
				for database, expected := range map[string][]string{
					"":               expectedAll,
					"customer":       {exact},
					"CUSTOMER":       nil,
					`"quoted db"`:    {quoted},
					"customer's db":  {apostrophe},
					"does-not-exist": nil,
				} {
					actual, err := query(timestamp, database, historyDB)
					Expect(err).NotTo(HaveOccurred(), database)
					Expect(actual).To(Equal(expected), database)
				}
			},
			Entry("before timestamp", GetBackupNamesBeforeTimestamp, "20240101120000",
				[]string{"20240101110000", "20240101100000", "20240101090000", "20240101080000"},
				"20240101110000", "20240101090000", "20240101080000"),
			Entry("after timestamp", GetBackupNamesAfterTimestamp, "20240101120000",
				[]string{"20240102110000", "20240102100000", "20240102090000", "20240102080000"},
				"20240102110000", "20240102090000", "20240102080000"),
			Entry("history clean", GetBackupNamesForCleanBeforeTimestamp, "20240101120000",
				[]string{"20240101110000-clean", "20240101100000-clean", "20240101090000-clean", "20240101080000-clean"},
				"20240101110000-clean", "20240101090000-clean", "20240101080000-clean"),
		)
	})

	Describe("deleteBackupsFormTableQuery", func() {
		It("returns correct query", func() {
			got := deleteBackupsFormTableQuery("TestBackup", "'20220401102430', '20220401102430'")
			Expect(got).To(Equal("DELETE FROM TestBackup WHERE timestamp IN ('20220401102430', '20220401102430');"))
		})
	})

	Describe("updateDeleteStatusQuery", func() {
		It("returns correct query", func() {
			got := updateDeleteStatusQuery("TestBackup", "20220401102430")
			Expect(got).To(Equal("UPDATE backups SET date_deleted = '20220401102430' WHERE timestamp = 'TestBackup';"))
		})
	})
})
