package cmd

import (
	"database/sql"
	"strconv"

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-backup/utils"
)

// Flags for the gpbackman backup-clean command (backupCleanCmd)
var (
	backupCleanBeforeTimestamp   string
	backupCleanAfterTimestamp    string
	backupCleanPluginConfigFile  string
	backupCleanBackupDir         string
	backupCleanOlderThanDays     uint
	backupCleanParallelProcesses int
	backupCleanCascade           bool
)

var backupCleanCmd = &cobra.Command{
	Use:   "backup-clean",
	Short: "Delete all existing backups older than the specified time condition",
	Long: `Delete all existing backups older than the specified time condition.

To delete backup sets older than the given timestamp, use the --before-timestamp option. 
To delete backup sets older than the given number of days, use the --older-than-day option.
To delete backup sets newer than the given timestamp, use the --after-timestamp option.
Only --older-than-days, --before-timestamp or --after-timestamp option must be specified.

By default, the existence of dependent backups is checked and deletion process is not performed,
unless the --cascade option is passed in.

By default, the deletion will be performed for local backup.

The full path to the backup directory can be set using the --backup-dir option.

For local backups the following logic are applied:
  * If the --backup-dir option is specified, the deletion will be performed in provided path.
  * If the --backup-dir option is not specified, but the backup was made with --backup-dir flag for gpbackup, the deletion will be performed in the backup manifest path.
  * If the --backup-dir option is not specified and backup directory is not specified in backup manifest, the deletion will be performed in backup folder in the master and segments data directories.
  * If backup is not local, the error will be returned.

For control over the number of parallel processes and ssh connections to delete local backups, the --parallel-processes option can be used.

The storage plugin config file location can be set using the --plugin-config option.
The full path to the file is required. In this case, the deletion will be performed using the storage plugin.

For non local backups the following logic are applied:
  * If the --plugin-config option is specified, the deletion will be performed using the storage plugin.
  * If backup is local, the error will be returned.

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database will be searched in the current directory.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doRootFlagValidation(cmd.Flags(), checkFileExistsConst)
		doCleanBackupFlagValidation(cmd.Flags())
		doCleanBackup()
	},
}

func init() {
	rootCmd.AddCommand(backupCleanCmd)
	backupCleanCmd.PersistentFlags().StringVar(
		&backupCleanPluginConfigFile,
		pluginConfigFileFlagName,
		"",
		"the full path to plugin config file",
	)
	backupCleanCmd.PersistentFlags().BoolVar(
		&backupCleanCascade,
		cascadeFlagName,
		false,
		"delete all dependent backups",
	)
	backupCleanCmd.PersistentFlags().UintVar(
		&backupCleanOlderThanDays,
		olderThanDaysFlagName,
		0,
		"delete backup sets older than the given number of days",
	)
	backupCleanCmd.PersistentFlags().StringVar(
		&backupCleanBeforeTimestamp,
		beforeTimestampFlagName,
		"",
		"delete backup sets older than the given timestamp",
	)
	backupCleanCmd.PersistentFlags().StringVar(
		&backupCleanAfterTimestamp,
		afterTimestampFlagName,
		"",
		"delete backup sets newer than the given timestamp",
	)
	backupCleanCmd.PersistentFlags().StringVar(
		&backupCleanBackupDir,
		backupDirFlagName,
		"",
		"the full path to backup directory for local backups",
	)
	backupCleanCmd.PersistentFlags().IntVar(
		&backupCleanParallelProcesses,
		parallelProcessesFlagName,
		1,
		"the number of parallel processes to delete local backups",
	)
	backupCleanCmd.MarkFlagsMutuallyExclusive(beforeTimestampFlagName, olderThanDaysFlagName, afterTimestampFlagName)
}

// These flag checks are applied only for backup-clean command.
func doCleanBackupFlagValidation(flags *pflag.FlagSet) {
	var err error
	// If before-timestamp flag is specified and have correct values.
	if flags.Changed(beforeTimestampFlagName) {
		err = gpbckpconfig.CheckTimestamp(backupCleanBeforeTimestamp)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(backupCleanBeforeTimestamp, beforeTimestampFlagName, err))
			execOSExit(exitErrorCode)
		}
		beforeTimestamp = backupCleanBeforeTimestamp
	}
	if flags.Changed(olderThanDaysFlagName) {
		beforeTimestamp = gpbckpconfig.GetTimestampOlderThan(backupCleanOlderThanDays)
	}
	// If after-timestamp flag is specified and have correct values.
	if flags.Changed(afterTimestampFlagName) {
		err = gpbckpconfig.CheckTimestamp(backupCleanAfterTimestamp)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(backupCleanAfterTimestamp, afterTimestampFlagName, err))
			execOSExit(exitErrorCode)
		}
		afterTimestamp = backupCleanAfterTimestamp
	}
	// backup-dir anf plugin-config flags cannot be used together.
	err = checkCompatibleFlags(flags, backupDirFlagName, pluginConfigFileFlagName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableCompatibleFlags(err, backupDirFlagName, pluginConfigFileFlagName))
		execOSExit(exitErrorCode)
	}
	// If parallel-processes flag is specified and have correct values.
	if flags.Changed(parallelProcessesFlagName) && !gpbckpconfig.IsPositiveValue(backupCleanParallelProcesses) {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(strconv.Itoa(backupCleanParallelProcesses), parallelProcessesFlagName, err))
		execOSExit(exitErrorCode)
	}
	// plugin-config and parallel-precesses flags cannot be used together.
	err = checkCompatibleFlags(flags, parallelProcessesFlagName, pluginConfigFileFlagName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableCompatibleFlags(err, parallelProcessesFlagName, pluginConfigFileFlagName))
		execOSExit(exitErrorCode)
	}
	// If backup-dir flag is specified and it exists and the full path is specified.
	if flags.Changed(backupDirFlagName) {
		err = gpbckpconfig.CheckFullPath(backupCleanBackupDir, checkFileExistsConst)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(backupCleanBackupDir, backupDirFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	// If plugin-config flag is specified and it exists and the full path is specified.
	if flags.Changed(pluginConfigFileFlagName) {
		err = gpbckpconfig.CheckFullPath(backupCleanPluginConfigFile, checkFileExistsConst)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(backupCleanPluginConfigFile, pluginConfigFileFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	if beforeTimestamp == "" && afterTimestamp == "" {
		gplog.Error("%s", textmsg.ErrorTextUnableValidateValue(textmsg.ErrorValidationValue(), olderThanDaysFlagName, beforeTimestampFlagName, afterTimestampFlagName))
		execOSExit(exitErrorCode)
	}
}

func doCleanBackup() {
	logHeadersDebug()
	err := cleanBackup()
	if err != nil {
		execOSExit(exitErrorCode)
	}
}

func cleanBackup() error {
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
	if backupCleanPluginConfigFile != "" {
		pluginConfig, err := utils.ReadPluginConfig(backupCleanPluginConfigFile)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableReadPluginConfigFile(err))
			return err
		}
		err = backupCleanDBPlugin(backupCleanCascade, beforeTimestamp, afterTimestamp, backupCleanPluginConfigFile, pluginConfig, hDB)
		if err != nil {
			return err
		}
	} else {
		err := backupCleanDBLocal(backupCleanCascade, beforeTimestamp, afterTimestamp, backupCleanBackupDir, backupCleanParallelProcesses, hDB)
		if err != nil {
			return err
		}
	}
	return nil
}

func backupCleanDBPlugin(deleteCascade bool, cutOffTimestamp, cutOffAfterTimestamp, pluginConfigPath string, pluginConfig *utils.PluginConfig, hDB *sql.DB) error {
	backupList, err := fetchBackupNamesForDeletion(cutOffTimestamp, cutOffAfterTimestamp, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableReadHistoryDB(err))
		return err
	}
	if len(backupList) > 0 {
		gplog.Debug("%s", textmsg.InfoTextBackupDeleteList(backupList))
		// Execute deletion for each backup.
		// Use backupDeleteDBPlugin function from backup-delete command.
		// Don't use force deletes and ignore errors for mass deletion.
		err = backupDeleteDBPlugin(backupList, deleteCascade, false, false, pluginConfigPath, pluginConfig, hDB)
		if err != nil {
			return err
		}
	} else {
		gplog.Info("%s", textmsg.InfoTextNothingToDo())
	}
	return nil
}

func backupCleanDBLocal(deleteCascade bool, cutOffTimestamp, cutOffAfterTimestamp, backupDir string, maxParallelProcesses int, hDB *sql.DB) error {
	backupList, err := fetchBackupNamesForDeletion(cutOffTimestamp, cutOffAfterTimestamp, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableReadHistoryDB(err))
		return err
	}
	if len(backupList) > 0 {
		gplog.Debug("%s", textmsg.InfoTextBackupDeleteList(backupList))
		err = backupDeleteDBLocal(backupList, backupDir, deleteCascade, false, false, maxParallelProcesses, hDB)
		if err != nil {
			return err
		}
	} else {
		gplog.Info("%s", textmsg.InfoTextNothingToDo())
	}
	return nil
}

// Get the list of backup names for deletion.
func fetchBackupNamesForDeletion(cutOffTimestamp, cutOffAfterTimestamp string, hDB *sql.DB) ([]string, error) {
	var backupList []string
	var err error
	if cutOffTimestamp != "" {
		backupList, err = gpbckpconfig.GetBackupNamesBeforeTimestamp(cutOffTimestamp, hDB)
		if err != nil {
			return nil, err
		}
	}
	if cutOffAfterTimestamp != "" {
		backupList, err = gpbckpconfig.GetBackupNamesAfterTimestamp(cutOffAfterTimestamp, hDB)
		if err != nil {
			return nil, err
		}
	}
	return backupList, nil
}
