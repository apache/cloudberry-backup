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
			Expect(getBackupNameBeforeTimestampQuery("20240101120000")).To(Equal(want))
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
			Expect(getBackupNameAfterTimestampQuery("20240101120000")).To(Equal(want))
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
			Expect(getBackupNameForCleanBeforeTimestampQuery("20240101120000")).To(Equal(want))
		})
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
