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

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
)

// Flags for the gpbackman history-clean command (historyCleanCmd)
var (
	historyCleanBeforeTimestamp      string
	historyCleanOlderThanDays        uint
	historyCleanNoHistorySyncStandby bool
	historyCleanDatabase             string
)

var historyCleanCmd = &cobra.Command{
	Use:   "history-clean",
	Short: "Clean deleted backups from the history database",
	Long: `Clean deleted backups from the history database.
Only the database is being cleaned up.

Information is deleted only about deleted backups from gpbackup_history.db. Each backup must be deleted first.

To delete information about backups older than the given timestamp, use the --before-timestamp option. 
To delete information about backups older than the given number of days, use the --older-than-day option. 
Only --older-than-days or --before-timestamp option must be specified, not both.

Use --database to clean history only for the specified database. Without --database,
cleanup includes deleted backup history for all databases in the history database.
Database names are matched exactly and case-sensitively against backup history.
For database names that require quoting, include the double quotes in the --database value.

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database is looked for in the current directory. To resolve it from $COORDINATOR_DATA_DIRECTORY instead, pass the --auto-load-history-db flag.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doRootFlagValidation(cmd.Flags(), checkFileExistsConst)
		doCleanHistoryFlagValidation(cmd.Flags())
		doCleanHistory()
	},
}

func init() {
	rootCmd.AddCommand(historyCleanCmd)
	historyCleanCmd.Flags().StringVar(
		&historyCleanDatabase,
		databaseFlagName,
		"",
		"delete backup history only for the specified database",
	)
	historyCleanCmd.PersistentFlags().UintVar(
		&historyCleanOlderThanDays,
		olderThanDaysFlagName,
		0,
		"delete information about backups older than the given number of days",
	)
	historyCleanCmd.PersistentFlags().StringVar(
		&historyCleanBeforeTimestamp,
		beforeTimestampFlagName,
		"",
		"delete information about backups older than the given timestamp",
	)
	historyCleanCmd.Flags().BoolVar(
		&historyCleanNoHistorySyncStandby,
		noHistorySyncStandbyFlagName,
		false,
		"skip automatic gpbackup_history.db sync to standby coordinator after this command",
	)
	historyCleanCmd.Flags().IntVar(
		&historyStandbySyncTimeoutSeconds,
		historySyncStandbyTimeoutFlagName,
		historySyncStandbyTimeoutDefault,
		"shared rsync and remote install timeout in seconds; must be an integer between 1 and 86400",
	)
	historyCleanCmd.MarkFlagsMutuallyExclusive(beforeTimestampFlagName, olderThanDaysFlagName)
}

// These flag checks are applied only for history-clean command.
func doCleanHistoryFlagValidation(flags *pflag.FlagSet) {
	var err error
	if flags.Changed(databaseFlagName) && historyCleanDatabase == "" {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(historyCleanDatabase, databaseFlagName, textmsg.ErrorEmptyDatabase()))
		execOSExit(exitErrorCode)
	}
	// If before-timestamp are specified and have correct values.
	if flags.Changed(beforeTimestampFlagName) {
		err = gpbckpconfig.CheckTimestamp(historyCleanBeforeTimestamp)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(historyCleanBeforeTimestamp, beforeTimestampFlagName, err))
			execOSExit(exitErrorCode)
		}
		beforeTimestamp = historyCleanBeforeTimestamp
	}
	if flags.Changed(olderThanDaysFlagName) {
		beforeTimestamp = gpbckpconfig.GetTimestampOlderThan(historyCleanOlderThanDays)
	}
	if beforeTimestamp == "" {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateValue(textmsg.ErrorValidationValue(), olderThanDaysFlagName, beforeTimestampFlagName))
		execOSExit(exitErrorCode)
	}
}

func doCleanHistory() {
	logHeadersDebug()
	runHistoryMutationWithStandbySync(cleanHistory, historyCleanNoHistorySyncStandby)
}

func cleanHistory() error {
	hDB, err := gpbckpconfig.OpenHistoryDB(getHistoryDBPath(rootHistoryDB, rootAutoLoadHistoryDB))
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableActionHistoryDB("open", err))
		return err
	}
	defer func() {
		closeErr := hDB.Close()
		if closeErr != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableActionHistoryDB("close", closeErr))
		}
	}()
	err = historyCleanDB(beforeTimestamp, historyCleanDatabase, hDB)
	if err != nil {
		return err
	}
	return nil
}

func historyCleanDB(cutOffTimestamp, databaseName string, hDB *sql.DB) error {
	backupList, err := gpbckpconfig.GetBackupNamesForCleanBeforeTimestamp(cutOffTimestamp, databaseName, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableReadHistoryDB(err))
		return err
	}
	if len(backupList) > 0 {
		gplog.Debug("%s", textmsg.InfoTextBackupDeleteListFromHistory(backupList))
		err := gpbckpconfig.CleanBackupsDB(backupList, sqliteDeleteBatchSize, hDB)
		if err != nil {
			return err
		}
	} else {
		gplog.Info("%s", textmsg.InfoTextNothingToDo())
	}
	return nil
}
