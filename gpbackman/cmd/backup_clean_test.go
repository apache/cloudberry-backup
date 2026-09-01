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

	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup-clean database filter", func() {
	It("registers a command-local database flag and documents its behavior", func() {
		flag := backupCleanCmd.Flags().Lookup(databaseFlagName)
		Expect(flag).NotTo(BeNil())
		Expect(flag.DefValue).To(Equal(""))
		Expect(flag.Usage).To(ContainSubstring("specified database"))
		Expect(backupCleanCmd.Long).To(ContainSubstring("Without --database"))
		Expect(backupCleanCmd.Long).To(ContainSubstring("case-sensitively"))
	})

	It("requires a value when the database flag is supplied", func() {
		rootCmd.SetArgs([]string{"backup-clean", "--" + databaseFlagName})
		DeferCleanup(func() { rootCmd.SetArgs(nil) })

		err := rootCmd.Execute()
		Expect(err).To(MatchError(ContainSubstring("flag needs an argument")))
	})

	DescribeTable("validates explicit empty database values",
		func(database string, setDatabase, wantExit bool) {
			oldDatabase := backupCleanDatabase
			oldCleanBeforeTimestamp := backupCleanBeforeTimestamp
			oldBeforeTimestamp := beforeTimestamp
			oldAfterTimestamp := afterTimestamp
			oldExecOSExit := execOSExit
			DeferCleanup(func() {
				backupCleanDatabase = oldDatabase
				backupCleanBeforeTimestamp = oldCleanBeforeTimestamp
				beforeTimestamp = oldBeforeTimestamp
				afterTimestamp = oldAfterTimestamp
				execOSExit = oldExecOSExit
			})

			backupCleanDatabase = database
			backupCleanBeforeTimestamp = "20240101120000"
			beforeTimestamp = ""
			afterTimestamp = ""
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String(beforeTimestampFlagName, "", "")
			flags.String(databaseFlagName, "", "")
			Expect(flags.Set(beforeTimestampFlagName, backupCleanBeforeTimestamp)).To(Succeed())
			if setDatabase {
				Expect(flags.Set(databaseFlagName, database)).To(Succeed())
			}

			exited := false
			execOSExit = func(code int) {
				Expect(code).To(Equal(exitErrorCode))
				exited = true
			}
			doCleanBackupFlagValidation(flags)
			Expect(exited).To(Equal(wantExit))
		},
		Entry("when absent", "", false, false),
		Entry("when non-empty", `"Customer's DB"`, true, false),
		Entry("when explicitly empty", "", true, true),
	)

	Describe("backup selection", func() {
		var historyDB *sql.DB

		BeforeEach(func() {
			var err error
			historyDB, err = sql.Open("sqlite3", "file:"+filepath.Join(GinkgoT().TempDir(), "history.db")+"?mode=rwc")
			Expect(err).NotTo(HaveOccurred())
			_, err = historyDB.Exec(`CREATE TABLE backups (timestamp TEXT, database_name TEXT, status TEXT, date_deleted TEXT)`)
			Expect(err).NotTo(HaveOccurred())
			for _, backup := range [][]string{
				{"20240101090000", "demo", "Success", ""},
				{"20240101100000", `"Customer's DB"`, "Success", ""},
				{"20240101110000", `"customer's db"`, "Success", ""},
				{"20240101130000", `"Customer's DB"`, "Success", ""},
			} {
				_, err = historyDB.Exec(`INSERT INTO backups (timestamp, database_name, status, date_deleted) VALUES (?, ?, ?, ?)`, backup[0], backup[1], backup[2], backup[3])
				Expect(err).NotTo(HaveOccurred())
			}
		})

		AfterEach(func() {
			Expect(historyDB.Close()).To(Succeed())
		})

		DescribeTable("filters before and after timestamp selections exactly",
			func(before, after, database string, expected []string) {
				actual, err := fetchBackupNamesForDeletion(before, after, database, historyDB)
				Expect(err).NotTo(HaveOccurred())
				Expect(actual).To(Equal(expected))
			},
			Entry("before timestamp exact database", "20240101120000", "", `"Customer's DB"`, []string{"20240101100000"}),
			Entry("after timestamp exact database", "", "20240101120000", `"Customer's DB"`, []string{"20240101130000"}),
			Entry("quoted case mismatch", "20240101120000", "", `"customer's db"`, []string{"20240101110000"}),
			Entry("unknown database", "20240101120000", "", "unknown", nil),
			Entry("without filter", "20240101120000", "", "", []string{"20240101110000", "20240101100000", "20240101090000"}),
		)

		It("does not invoke local or plugin cleanup for an unknown database", func() {
			Expect(backupCleanDBLocal(false, "20240101120000", "", "unknown", "", 1, historyDB)).To(Succeed())
			Expect(backupCleanDBPlugin(false, "20240101120000", "", "unknown", "", nil, historyDB)).To(Succeed())
		})
	})
})
