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
	"fmt"

	"github.com/apache/cloudberry-backup/history"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("utils_db tests", func() {
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
