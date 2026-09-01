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
	"path/filepath"

	"github.com/apache/cloudberry-backup/history"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("history-clean database filter", func() {
	It("registers a command-local database flag and documents its behavior", func() {
		flag := historyCleanCmd.Flags().Lookup(databaseFlagName)
		Expect(flag).NotTo(BeNil())
		Expect(flag.DefValue).To(Equal(""))
		Expect(flag.Usage).To(ContainSubstring("specified database"))
		Expect(historyCleanCmd.Long).To(ContainSubstring("Without --database"))
		Expect(historyCleanCmd.Long).To(ContainSubstring("case-sensitively"))
	})

	It("requires a value when the database flag is supplied", func() {
		rootCmd.SetArgs([]string{"history-clean", "--" + databaseFlagName})
		DeferCleanup(func() { rootCmd.SetArgs(nil) })

		err := rootCmd.Execute()
		Expect(err).To(MatchError(ContainSubstring("flag needs an argument")))
	})

	DescribeTable("validates explicit empty database values",
		func(database string, setDatabase, olderThan, wantExit bool) {
			oldDatabase := historyCleanDatabase
			oldCleanBeforeTimestamp := historyCleanBeforeTimestamp
			oldCleanOlderThanDays := historyCleanOlderThanDays
			oldBeforeTimestamp := beforeTimestamp
			oldExecOSExit := execOSExit
			DeferCleanup(func() {
				historyCleanDatabase = oldDatabase
				historyCleanBeforeTimestamp = oldCleanBeforeTimestamp
				historyCleanOlderThanDays = oldCleanOlderThanDays
				beforeTimestamp = oldBeforeTimestamp
				execOSExit = oldExecOSExit
			})

			historyCleanDatabase = database
			historyCleanBeforeTimestamp = "20240101120000"
			historyCleanOlderThanDays = 1
			beforeTimestamp = ""
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String(beforeTimestampFlagName, "", "")
			flags.Uint(olderThanDaysFlagName, 0, "")
			flags.String(databaseFlagName, "", "")
			if olderThan {
				Expect(flags.Set(olderThanDaysFlagName, "1")).To(Succeed())
			} else {
				Expect(flags.Set(beforeTimestampFlagName, historyCleanBeforeTimestamp)).To(Succeed())
			}
			if setDatabase {
				Expect(flags.Set(databaseFlagName, database)).To(Succeed())
			}

			exited := false
			execOSExit = func(code int) {
				Expect(code).To(Equal(exitErrorCode))
				exited = true
			}
			doCleanHistoryFlagValidation(flags)
			Expect(exited).To(Equal(wantExit))
			if olderThan {
				Expect(beforeTimestamp).NotTo(BeEmpty())
			}
		},
		Entry("when absent", "", false, false, false),
		Entry("when non-empty", `"Customer's DB"`, true, false, false),
		Entry("when explicitly empty", "", true, false, true),
		Entry("with older-than-days", "demo", true, true, false),
	)

	It("cleans only the selected database and its related history rows", func() {
		historyDB := createHistoryCleanTestDB()
		selectedDeleted := historyCleanTestConfig("20240101000000", `"Customer's DB"`, "20240102000000")
		otherDeleted := historyCleanTestConfig("20240101010000", `"customer's db"`, "20240102000000")
		selectedNew := historyCleanTestConfig("20240103000000", `"Customer's DB"`, "20240104000000")
		selectedActive := historyCleanTestConfig("20240101020000", `"Customer's DB"`, "")
		selectedFailed := historyCleanTestConfig("20240101030000", `"Customer's DB"`, "")
		selectedFailed.Status = history.BackupStatusFailed
		storeHistoryCleanConfigs(historyDB, selectedDeleted, otherDeleted, selectedNew, selectedActive, selectedFailed)

		Expect(historyCleanDB("20240102000000", `"Customer's DB"`, historyDB)).To(Succeed())

		assertHistoryCleanTimestampExists(historyDB, selectedDeleted.Timestamp, false)
		for _, backup := range []history.BackupConfig{otherDeleted, selectedNew, selectedActive, selectedFailed} {
			assertHistoryCleanTimestampExists(historyDB, backup.Timestamp, true)
		}
	})

	It("leaves unknown databases untouched and cleans all databases without a filter", func() {
		historyDB := createHistoryCleanTestDB()
		first := historyCleanTestConfig("20240101000000", "demo", "20240102000000")
		second := historyCleanTestConfig("20240101010000", "other", "20240102000000")
		storeHistoryCleanConfigs(historyDB, first, second)

		Expect(historyCleanDB("20240102000000", "unknown", historyDB)).To(Succeed())
		assertHistoryCleanTimestampExists(historyDB, first.Timestamp, true)
		assertHistoryCleanTimestampExists(historyDB, second.Timestamp, true)

		Expect(historyCleanDB("20240102000000", "", historyDB)).To(Succeed())
		assertHistoryCleanTimestampExists(historyDB, first.Timestamp, false)
		assertHistoryCleanTimestampExists(historyDB, second.Timestamp, false)
	})
})

var historyCleanAuxiliaryTables = []string{
	"backups",
	"restore_plans",
	"restore_plan_tables",
	"exclude_relations",
	"exclude_schemas",
	"include_relations",
	"include_schemas",
}

func createHistoryCleanTestDB() *sql.DB {
	historyDB, err := history.InitializeHistoryDatabase(filepath.Join(GinkgoT().TempDir(), "gpbackup_history.db"))
	Expect(err).NotTo(HaveOccurred())
	_, err = historyDB.Exec("PRAGMA foreign_keys = OFF;")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(historyDB.Close()).To(Succeed()) })
	return historyDB
}

func historyCleanTestConfig(timestamp, databaseName, dateDeleted string) history.BackupConfig {
	return history.BackupConfig{
		Timestamp:        timestamp,
		DatabaseName:     databaseName,
		DateDeleted:      dateDeleted,
		Status:           history.BackupStatusSucceed,
		ExcludeRelations: []string{"public.excluded_relation"},
		ExcludeSchemas:   []string{"excluded_schema"},
		IncludeRelations: []string{"public.included_relation"},
		IncludeSchemas:   []string{"included_schema"},
		RestorePlan: []history.RestorePlanEntry{{
			Timestamp: "20230101000000",
			TableFQNs: []string{"public.restored_table"},
		}},
	}
}

func storeHistoryCleanConfigs(historyDB *sql.DB, configs ...history.BackupConfig) {
	for i := range configs {
		Expect(history.StoreBackupHistory(historyDB, &configs[i])).To(Succeed())
	}
}

func assertHistoryCleanTimestampExists(historyDB *sql.DB, timestamp string, want bool) {
	for _, table := range historyCleanAuxiliaryTables {
		var count int
		Expect(historyDB.QueryRow("SELECT COUNT(*) FROM "+table+" WHERE timestamp = ?", timestamp).Scan(&count)).To(Succeed())
		Expect(count > 0).To(Equal(want), table)
	}
}
