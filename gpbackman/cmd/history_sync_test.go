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
	"bytes"
	"errors"
	"os"

	"github.com/apache/cloudberry-go-libs/testhelper"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("history sync command", func() {
	Describe("command registration", func() {
		AfterEach(func() {
			rootCmd.SetOut(os.Stdout)
			rootCmd.SetErr(os.Stderr)
		})

		It("shows history-sync in root help without exposing the automatic disable flag", func() {
			var output bytes.Buffer
			rootCmd.SetOut(&output)
			rootCmd.SetErr(&output)

			Expect(rootCmd.Help()).To(Succeed())

			help := output.String()
			Expect(help).To(ContainSubstring("history-sync"))
			Expect(help).ToNot(ContainSubstring(noHistorySyncStandbyFlagName))
		})

		It("registers no-history-sync-standby only on mutation commands", func() {
			mutationCommands := map[string]bool{
				"backup-delete": true,
				"backup-clean":  true,
				"history-clean": true,
			}

			for _, command := range rootCmd.Commands() {
				flag := command.Flags().Lookup(noHistorySyncStandbyFlagName)
				if mutationCommands[command.Name()] {
					Expect(flag).ToNot(BeNil(), command.Name())
					Expect(flag.DefValue).To(Equal("false"), command.Name())
					continue
				}
				Expect(flag).To(BeNil(), command.Name())
			}
			Expect(rootCmd.PersistentFlags().Lookup(noHistorySyncStandbyFlagName)).To(BeNil())
		})

		It("keeps history-sync strict with inherited global flags and no local flags", func() {
			Expect(commandByName("history-sync")).To(Equal(historySyncCmd))
			Expect(historySyncCmd.Args(historySyncCmd, []string{"unexpected"})).To(HaveOccurred())
			Expect(flagNames(historySyncCmd.LocalFlags())).To(BeEmpty())
			Expect(historySyncCmd.Flags().Lookup(noHistorySyncStandbyFlagName)).To(BeNil())
			for _, flagName := range []string{
				historyDBFlagName,
				autoLoadHistoryDBFlagName,
				logFileFlagName,
				logLevelConsoleFlagName,
				logLevelFileFlagName,
			} {
				Expect(historySyncCmd.Flag(flagName)).ToNot(BeNil(), flagName)
			}
		})
	})

	Describe("strict history sync execution", func() {
		var (
			originalHistoryStandbySync func() historyStandbySyncResult
			originalExecOSExit         func(int)
			savedRootHistoryDB         string
			savedRootAutoLoadHistoryDB bool
			savedPGPassword            string
			savedPGPasswordPresent     bool
			exitCodes                  []int
		)

		BeforeEach(func() {
			testhelper.SetupTestLogger()
			originalHistoryStandbySync = historyStandbySync
			originalExecOSExit = execOSExit
			savedRootHistoryDB = rootHistoryDB
			savedRootAutoLoadHistoryDB = rootAutoLoadHistoryDB
			savedPGPassword, savedPGPasswordPresent = os.LookupEnv("PGPASSWORD")
			Expect(os.Setenv("PGPASSWORD", "do-not-log-this-password")).To(Succeed())
			exitCodes = make([]int, 0)
			execOSExit = func(code int) {
				exitCodes = append(exitCodes, code)
			}
			rootHistoryDB = ""
			rootAutoLoadHistoryDB = false
		})

		AfterEach(func() {
			historyStandbySync = originalHistoryStandbySync
			execOSExit = originalExecOSExit
			rootHistoryDB = savedRootHistoryDB
			rootAutoLoadHistoryDB = savedRootAutoLoadHistoryDB
			if savedPGPasswordPresent {
				Expect(os.Setenv("PGPASSWORD", savedPGPassword)).To(Succeed())
			} else {
				Expect(os.Unsetenv("PGPASSWORD")).To(Succeed())
			}
		})

		It("exits zero only when strict sync succeeds", func() {
			historyStandbySync = func() historyStandbySyncResult {
				return historyStandbySyncResult{}
			}

			doHistorySync()

			Expect(exitCodes).To(BeEmpty())
		})

		It("treats the default working-directory source as a strict error", func() {
			stdout, stderr, _ := testhelper.SetupTestLogger()
			historyStandbySync = syncHistoryStandby

			doHistorySync()

			logOutput := string(stdout.Contents()) + string(stderr.Contents())
			Expect(exitCodes).To(Equal([]int{exitErrorCode}))
			Expect(logOutput).To(ContainSubstring("Unable to sync history db to standby coordinator"))
			Expect(logOutput).To(ContainSubstring("using default working-directory history db"))
			Expect(logOutput).ToNot(ContainSubstring("do-not-log-this-password"))
		})

		It("exits with one for strict skip and stage errors", func() {
			tests := []struct {
				name   string
				result historyStandbySyncResult
				want   string
			}{
				{
					name:   "custom source",
					result: historyStandbySyncResult{skipReason: "source history db /custom/gpbackup_history.db is not cluster history db /primary/gpbackup_history.db"},
					want:   "source history db /custom/gpbackup_history.db is not cluster history db /primary/gpbackup_history.db",
				},
				{
					name:   "no standby",
					result: historyStandbySyncResult{skipReason: "no up standby coordinator found"},
					want:   "no up standby coordinator found",
				},
				{
					name:   "busy lock",
					result: historyStandbySyncResult{err: errors.New("lock standby history sync source /primary/gpbackup_history.db: already locked")},
					want:   "lock standby history sync source /primary/gpbackup_history.db",
				},
				{
					name:   "stage error",
					result: historyStandbySyncResult{err: errors.New("validate standby history sync snapshot quick_check failed")},
					want:   "validate standby history sync snapshot quick_check failed",
				},
			}

			for _, tt := range tests {
				stdout, stderr, _ := testhelper.SetupTestLogger()
				exitCodes = make([]int, 0)
				result := tt.result
				historyStandbySync = func() historyStandbySyncResult {
					return result
				}

				doHistorySync()

				logOutput := string(stdout.Contents()) + string(stderr.Contents())
				Expect(exitCodes).To(Equal([]int{exitErrorCode}), tt.name)
				Expect(logOutput).To(ContainSubstring("Unable to sync history db to standby coordinator"), tt.name)
				Expect(logOutput).To(ContainSubstring(tt.want), tt.name)
				Expect(logOutput).ToNot(ContainSubstring("do-not-log-this-password"), tt.name)
			}
		})
	})

	Describe("mutation command automatic sync hooks", func() {
		var (
			originalRunHistoryMutationWithStandbySync func(func() error, bool)
			savedBackupDeleteNoHistorySyncStandby     bool
			savedBackupCleanNoHistorySyncStandby      bool
			savedHistoryCleanNoHistorySyncStandby     bool
		)

		BeforeEach(func() {
			originalRunHistoryMutationWithStandbySync = runHistoryMutationWithStandbySync
			savedBackupDeleteNoHistorySyncStandby = backupDeleteNoHistorySyncStandby
			savedBackupCleanNoHistorySyncStandby = backupCleanNoHistorySyncStandby
			savedHistoryCleanNoHistorySyncStandby = historyCleanNoHistorySyncStandby
		})

		AfterEach(func() {
			runHistoryMutationWithStandbySync = originalRunHistoryMutationWithStandbySync
			backupDeleteNoHistorySyncStandby = savedBackupDeleteNoHistorySyncStandby
			backupCleanNoHistorySyncStandby = savedBackupCleanNoHistorySyncStandby
			historyCleanNoHistorySyncStandby = savedHistoryCleanNoHistorySyncStandby
		})

		It("wraps all mutation commands and passes their disable flag values", func() {
			disabledValues := make([]bool, 0)
			runHistoryMutationWithStandbySync = func(work func() error, disabled bool) {
				disabledValues = append(disabledValues, disabled)
			}
			backupDeleteNoHistorySyncStandby = true
			backupCleanNoHistorySyncStandby = false
			historyCleanNoHistorySyncStandby = true

			doDeleteBackup()
			doCleanBackup()
			doCleanHistory()

			Expect(disabledValues).To(Equal([]bool{true, false, true}))
		})
	})
})

func commandByName(name string) *cobra.Command {
	for _, command := range rootCmd.Commands() {
		if command.Name() == name {
			return command
		}
	}
	return nil
}

func flagNames(flags *pflag.FlagSet) []string {
	names := make([]string, 0)
	flags.VisitAll(func(flag *pflag.Flag) {
		names = append(names, flag.Name)
	})
	return names
}
