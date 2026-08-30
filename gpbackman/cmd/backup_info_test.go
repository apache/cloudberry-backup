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
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup-info database filter", func() {
	It("registers and documents the command-local database flag", func() {
		flag := backupInfoCmd.Flags().Lookup(databaseFlagName)
		Expect(flag).NotTo(BeNil())
		Expect(flag.DefValue).To(Equal(""))
		Expect(flag.Usage).To(ContainSubstring("specified database"))
		Expect(backupInfoCmd.Long).To(ContainSubstring("Without the --database option, backups for all databases are displayed."))
		Expect(backupInfoCmd.Long).To(ContainSubstring("Database names are matched exactly and case-sensitively against backup history."))
		Expect(backupInfoCmd.Long).To(ContainSubstring("include the double quotes in the flag value"))
		Expect(backupInfoCmd.Long).To(ContainSubstring("The --detail option can be used with --timestamp"))
		Expect(backupInfoCmd.Long).To(ContainSubstring("--database, --type, --table, --schema, --exclude, --failed, --deleted"))
		Expect(backupInfoCmd.UsageString()).To(ContainSubstring("--" + databaseFlagName + " string"))
	})

	It("requires a value when the database flag is supplied", func() {
		rootCmd.SetArgs([]string{"backup-info", "--" + databaseFlagName})
		DeferCleanup(func() { rootCmd.SetArgs(nil) })

		Expect(rootCmd.Execute()).To(MatchError(ContainSubstring("flag needs an argument")))
	})

	DescribeTable("validates explicit database values",
		func(database string, setDatabase, wantExit bool) {
			oldDatabase := backupInfoDatabase
			oldTimestamp := backupInfoTimestamp
			oldExecOSExit := execOSExit
			DeferCleanup(func() {
				backupInfoDatabase = oldDatabase
				backupInfoTimestamp = oldTimestamp
				execOSExit = oldExecOSExit
			})

			backupInfoDatabase = database
			backupInfoTimestamp = ""
			flags := pflag.NewFlagSet("backup-info", pflag.ContinueOnError)
			flags.String(databaseFlagName, "", "")
			if setDatabase {
				Expect(flags.Set(databaseFlagName, database)).To(Succeed())
			}

			exited := false
			execOSExit = func(code int) {
				Expect(code).To(Equal(exitErrorCode))
				exited = true
			}
			doBackupInfoFlagValidation(flags)
			Expect(exited).To(Equal(wantExit))
		},
		Entry("when absent", "", false, false),
		Entry("when non-empty", `"Customer's DB"`, true, false),
		Entry("when explicitly empty", "", true, true),
	)

	DescribeTable("preserves timestamp and detail compatibility",
		func(setDatabase, setTimestamp, setDetail, wantExit bool) {
			oldDatabase := backupInfoDatabase
			oldTimestamp := backupInfoTimestamp
			oldExecOSExit := execOSExit
			DeferCleanup(func() {
				backupInfoDatabase = oldDatabase
				backupInfoTimestamp = oldTimestamp
				execOSExit = oldExecOSExit
			})

			backupInfoDatabase = `"Customer's DB"`
			backupInfoTimestamp = "20240101120000"
			flags := pflag.NewFlagSet("backup-info", pflag.ContinueOnError)
			flags.String(databaseFlagName, "", "")
			flags.String(timestampFlagName, "", "")
			flags.Bool(detailFlagName, false, "")
			if setDatabase {
				Expect(flags.Set(databaseFlagName, backupInfoDatabase)).To(Succeed())
			}
			if setTimestamp {
				Expect(flags.Set(timestampFlagName, backupInfoTimestamp)).To(Succeed())
			}
			if setDetail {
				Expect(flags.Set(detailFlagName, "true")).To(Succeed())
			}

			exited := false
			execOSExit = func(code int) {
				Expect(code).To(Equal(exitErrorCode))
				exited = true
			}
			doBackupInfoFlagValidation(flags)
			Expect(exited).To(Equal(wantExit))
		},
		Entry("database with detail", true, false, true, false),
		Entry("timestamp with detail", false, true, true, false),
		Entry("database with timestamp", true, true, false, true),
		Entry("database with timestamp and detail", true, true, true, true),
	)

	DescribeTable("matches database names exactly before other filters",
		func(databaseFilter, backupDatabase string, wantRows int) {
			t := tablewriter.NewWriter(GinkgoWriter)
			backupData := backupInfoTestConfig("20240101000000", backupDatabase)
			addBackupToTable("", "", "", databaseFilter, false, false, &backupData, t)
			Expect(t.NumLines()).To(Equal(wantRows))
		},
		Entry("without a filter", "", "demo", 1),
		Entry("with an exact match", "demo", "demo", 1),
		Entry("with a case mismatch", "Demo", "demo", 0),
		Entry("with a quoted name", `"Customer's DB"`, `"Customer's DB"`, 1),
		Entry("with an unknown database", "unknown", "demo", 0),
	)

	It("displays matching backups when their derived fields are invalid", func() {
		t := tablewriter.NewWriter(GinkgoWriter)
		backupData := backupInfoTestConfig("invalid", "demo")
		backupData.EndTime = "also-invalid"
		backupData.Incremental = true
		backupData.DataOnly = true
		backupData.IncludeSchemaFiltered = true
		backupData.IncludeTableFiltered = true
		backupData.DateDeleted = "invalid"

		addBackupToTable("", "", "", "demo", false, false, &backupData, t)
		Expect(t.NumLines()).To(Equal(1))
	})

	It("composes database filtering with type, table, schema, and detail filters", func() {
		matchingTable := backupInfoTestConfig("20240101000000", "demo")
		matchingTable.IncludeTableFiltered = true
		matchingTable.IncludeRelations = []string{"public.orders"}
		matchingSchema := backupInfoTestConfig("20240101000001", "demo")
		matchingSchema.IncludeSchemaFiltered = true
		matchingSchema.IncludeSchemas = []string{"public"}
		wrongType := backupInfoTestConfig("20240101000002", "demo")
		wrongType.Incremental = true
		wrongObject := backupInfoTestConfig("20240101000003", "demo")
		wrongObject.IncludeTableFiltered = true
		wrongObject.IncludeRelations = []string{"public.customers"}
		otherDatabase := backupInfoTestConfig("20240101000004", "other")
		otherDatabase.IncludeTableFiltered = true
		otherDatabase.IncludeRelations = []string{"public.orders"}
		configs := []*history.BackupConfig{&matchingTable, &matchingSchema, &wrongType, &wrongObject, &otherDatabase}

		for _, test := range []struct {
			name, typeFilter, tableFilter, schemaFilter string
			includeDetail                               bool
			wantRows                                    int
		}{
			{"type", "full", "", "", false, 3},
			{"table", "", "public.orders", "", false, 1},
			{"schema", "", "", "public", false, 1},
			{"database and type", "full", "", "", false, 3},
			{"database and table", "", "public.orders", "", false, 1},
			{"database and schema", "", "", "public", false, 1},
			{"database and detail", "full", "", "", true, 3},
		} {
			t := tablewriter.NewWriter(GinkgoWriter)
			for _, config := range configs {
				addBackupToTable(test.typeFilter, test.tableFilter, test.schemaFilter, "demo", false, test.includeDetail, config, t)
			}
			Expect(t.NumLines()).To(Equal(test.wantRows), test.name)
		}
	})

	It("filters database output and leaves timestamp chains unfiltered", func() {
		baseTimestamp := "20240101000000"
		historyDB := createBackupInfoTestDB(
			backupInfoTestConfig(baseTimestamp, "demo"),
			backupInfoTestConfig("20240101000001", "other"),
			backupInfoTestConfigWithDependency("20240101000002", "demo", baseTimestamp),
			backupInfoTestConfigWithDependency("20240101000003", `"Customer's DB"`, baseTimestamp),
		)

		filtered := tablewriter.NewWriter(GinkgoWriter)
		Expect(backupInfoDB(BackupInfoOptions{DatabaseFilter: "demo"}, historyDB, filtered)).To(Succeed())
		Expect(filtered.NumLines()).To(Equal(2))

		unknown := tablewriter.NewWriter(GinkgoWriter)
		Expect(backupInfoDB(BackupInfoOptions{DatabaseFilter: "unknown"}, historyDB, unknown)).To(Succeed())
		Expect(unknown.NumLines()).To(Equal(0))

		chain := tablewriter.NewWriter(GinkgoWriter)
		Expect(backupInfoDB(BackupInfoOptions{Timestamp: baseTimestamp, ShowDetails: true}, historyDB, chain)).To(Succeed())
		Expect(chain.NumLines()).To(Equal(3))
	})
})

func backupInfoTestConfig(timestamp, databaseName string) history.BackupConfig {
	return history.BackupConfig{
		Timestamp:    timestamp,
		EndTime:      timestamp,
		DatabaseName: databaseName,
		Status:       history.BackupStatusSucceed,
	}
}

func backupInfoTestConfigWithDependency(timestamp, databaseName, baseTimestamp string) history.BackupConfig {
	config := backupInfoTestConfig(timestamp, databaseName)
	config.RestorePlan = []history.RestorePlanEntry{{Timestamp: baseTimestamp}}
	return config
}

func createBackupInfoTestDB(configs ...history.BackupConfig) *sql.DB {
	historyDB, err := history.InitializeHistoryDatabase(filepath.Join(GinkgoT().TempDir(), historyDBNameConst))
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(historyDB.Close()).To(Succeed()) })
	for i := range configs {
		Expect(history.StoreBackupHistory(historyDB, &configs[i])).To(Succeed())
	}
	return historyDB
}
