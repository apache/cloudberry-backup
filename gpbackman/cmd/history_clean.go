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
	historyCleanBeforeTimestamp string
	historyCleanOlderThenDays   uint
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

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database will be searched in the current directory.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doRootFlagValidation(cmd.Flags(), checkFileExistsConst)
		doCleanHistoryFlagValidation(cmd.Flags())
		doCleanHistory()
	},
}

func init() {
	rootCmd.AddCommand(historyCleanCmd)
	historyCleanCmd.PersistentFlags().UintVar(
		&historyCleanOlderThenDays,
		olderThenDaysFlagName,
		0,
		"delete information about backups older than the given number of days",
	)
	historyCleanCmd.PersistentFlags().StringVar(
		&historyCleanBeforeTimestamp,
		beforeTimestampFlagName,
		"",
		"delete information about backups older than the given timestamp",
	)
	historyCleanCmd.MarkFlagsMutuallyExclusive(beforeTimestampFlagName, olderThenDaysFlagName)
}

// These flag checks are applied only for backup-clean command.
func doCleanHistoryFlagValidation(flags *pflag.FlagSet) {
	var err error
	// If before-timestamp are specified and have correct values.
	if flags.Changed(beforeTimestampFlagName) {
		err = gpbckpconfig.CheckTimestamp(historyCleanBeforeTimestamp)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(historyCleanBeforeTimestamp, beforeTimestampFlagName, err))
			execOSExit(exitErrorCode)
		}
		beforeTimestamp = historyCleanBeforeTimestamp
	}
	if flags.Changed(olderThenDaysFlagName) {
		beforeTimestamp = gpbckpconfig.GetTimestampOlderThen(historyCleanOlderThenDays)
	}
	if beforeTimestamp == "" {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateValue(textmsg.ErrorValidationValue(), olderThenDaysFlagName, beforeTimestampFlagName))
		execOSExit(exitErrorCode)
	}
}

func doCleanHistory() {
	logHeadersDebug()
	err := cleanHistory()
	if err != nil {
		execOSExit(exitErrorCode)
	}
}

func cleanHistory() error {
	hDB, err := gpbckpconfig.OpenHistoryDB(getHistoryDBPath(rootHistoryDB))
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
	err = historyCleanDB(beforeTimestamp, hDB)
	if err != nil {
		return err
	}
	return nil
}

func historyCleanDB(cutOffTimestamp string, hDB *sql.DB) error {
	backupList, err := gpbckpconfig.GetBackupNamesForCleanBeforeTimestamp(cutOffTimestamp, hDB)
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
