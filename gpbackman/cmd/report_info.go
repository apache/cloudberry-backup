package cmd

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-backup/history"
	"github.com/apache/cloudberry-backup/utils"
)

// Flags for the gpbackman report-info command (reportInfoCmd)
var (
	reportInfoTimestamp            string
	reportInfoPluginConfigFile     string
	reportInfoReportFilePluginPath string
	reportInfoBackupDir            string
)

var reportInfoCmd = &cobra.Command{
	Use:   "report-info",
	Short: "Display the report for a specific backup",
	Long: `Display the report for a specific backup.

The --timestamp option must be specified.

The report could be displayed only for active backups.

The full path to the backup directory can be set using the --backup-dir option.
The full path to the data directory is required.

For local backups the following logic are applied:
  * If the --backup-dir option is specified, the report will be searched in provided path.
  * If the --backup-dir option is not specified, but the backup was made with --backup-dir flag for gpbackup, the report will be searched in provided path from backup manifest.
  * If the --backup-dir option is not specified and backup directory is not specified in backup manifest, the utility try to connect to local cluster and get master data directory.
    If this information is available, the report will be in master data directory.
  * If backup is not local, the error will be returned.

The storage plugin config file location can be set using the --plugin-config option.
The full path to the file is required.

For non local backups the following logic are applied:
  * If the --plugin-config option is specified, the report will be searched in provided location.
  * If backup is local, the error will be returned.

Only --backup-dir or --plugin-config option can be specified, not both.

If a custom plugin is used, it is required to specify the path to the directory with the repo file using the --plugin-report-file-path option.
It is not necessary to use the --plugin-report-file-path flag for the following plugins (the path is generated automatically):
  * gpbackup_s3_plugin.

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database will be searched in the current directory.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doRootFlagValidation(cmd.Flags(), checkFileExistsConst)
		doReportInfoFlagValidation(cmd.Flags())
		doReportInfo()
	},
}

func init() {
	rootCmd.AddCommand(reportInfoCmd)
	reportInfoCmd.PersistentFlags().StringVar(
		&reportInfoTimestamp,
		timestampFlagName,
		"",
		"the backup timestamp for report displaying",
	)
	reportInfoCmd.PersistentFlags().StringVar(
		&reportInfoPluginConfigFile,
		pluginConfigFileFlagName,
		"",
		"the full path to plugin config file",
	)
	reportInfoCmd.PersistentFlags().StringVar(
		&reportInfoReportFilePluginPath,
		reportFilePluginPathFlagName,
		"",
		"the full path to plugin report file",
	)
	reportInfoCmd.PersistentFlags().StringVar(
		&reportInfoBackupDir,
		backupDirFlagName,
		"",
		"the full path to backup directory",
	)
	_ = reportInfoCmd.MarkPersistentFlagRequired(timestampFlagName)
}

// These flag checks are applied only for report-info command.
func doReportInfoFlagValidation(flags *pflag.FlagSet) {
	var err error
	// If timestamps are specified and have correct values.
	if flags.Changed(timestampFlagName) {
		err = gpbckpconfig.CheckTimestamp(reportInfoTimestamp)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(reportInfoTimestamp, timestampFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	// backup-dir anf plugin-config flags cannot be used together.
	err = checkCompatibleFlags(flags, backupDirFlagName, pluginConfigFileFlagName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableCompatibleFlags(err, backupDirFlagName, pluginConfigFileFlagName))
		execOSExit(exitErrorCode)
	}
	// If backup-dir flag is specified and it exists and the full path is specified.
	if flags.Changed(backupDirFlagName) {
		err = gpbckpconfig.CheckFullPath(reportInfoBackupDir, checkFileExistsConst)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(reportInfoBackupDir, backupDirFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	// If plugin-config flag is specified and it exists and the full path is specified.
	if flags.Changed(pluginConfigFileFlagName) {
		err = gpbckpconfig.CheckFullPath(reportInfoPluginConfigFile, checkFileExistsConst)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(reportInfoPluginConfigFile, pluginConfigFileFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
	// If plugin-report-file-path flag is specified.
	if flags.Changed(reportFilePluginPathFlagName) {
		// But plugin-config flag is not specified.
		if !flags.Changed(pluginConfigFileFlagName) {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateValue(textmsg.ErrorNotIndependentFlagsError(), reportFilePluginPathFlagName, pluginConfigFileFlagName))
			execOSExit(exitErrorCode)
		}
		// Check full path.
		err = gpbckpconfig.CheckFullPath(reportInfoReportFilePluginPath, false)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableValidateFlag(reportInfoReportFilePluginPath, reportFilePluginPathFlagName, err))
			execOSExit(exitErrorCode)
		}
	}
}

func doReportInfo() {
	logHeadersDebug()
	err := reportInfo()
	if err != nil {
		execOSExit(exitErrorCode)
	}
}

func reportInfo() error {
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
	if reportInfoPluginConfigFile != "" {
		pluginConfig, err := utils.ReadPluginConfig(reportInfoPluginConfigFile)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableReadPluginConfigFile(err))
			return err
		}
		err = reportInfoDBPlugin(reportInfoTimestamp, reportInfoPluginConfigFile, pluginConfig, hDB)
		if err != nil {
			return err
		}
	} else {
		err := reportInfoDBLocal(reportInfoTimestamp, reportInfoBackupDir, hDB)
		if err != nil {
			return err
		}
	}
	return nil
}

func reportInfoDBPlugin(backupName, pluginConfigPath string, pluginConfig *utils.PluginConfig, hDB *sql.DB) error {
	backupData, err := gpbckpconfig.GetBackupDataDB(backupName, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableGetBackupInfo(backupName, err))
		return err
	}
	err = reportInfoPluginFunc(backupData, pluginConfigPath, pluginConfig)
	if err != nil {
		return err
	}
	return nil
}

func reportInfoPluginFunc(backupData *history.BackupConfig, pluginConfigPath string, pluginConfig *utils.PluginConfig) error {
	// Skip local backup.
	canGetReport, err := checkBackupCanBeUsed(false, true, backupData)
	if err != nil {
		return err
	}
	if canGetReport {
		reportFile, err := gpbckpconfig.GetReportFilePathPlugin(backupData, reportInfoReportFilePluginPath, pluginConfig.Options)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableGetBackupPath("report", backupData.Timestamp, err))
			return err
		}
		gplog.Debug("%s", textmsg.InfoTextCommandExecution(pluginConfig.ExecutablePath, restoreDataPluginCommand, pluginConfigPath, reportFile))
		stdout, stderr, err := execReportInfo(pluginConfig.ExecutablePath, restoreDataPluginCommand, pluginConfigPath, reportFile)
		if stderr != "" {
			gplog.Error("%s", stderr)
		}
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableGetBackupReport(backupData.Timestamp, err))
			return err
		}
		// Display the report.
		fmt.Println(stdout)
	}
	return nil
}

func reportInfoDBLocal(backupName, backupDir string, hDB *sql.DB) error {
	backupData, err := gpbckpconfig.GetBackupDataDB(backupName, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableGetBackupInfo(backupName, err))
		return err
	}
	err = reportInfoFileLocalFunc(backupData, backupDir)
	if err != nil {
		return err
	}
	return nil
}

func reportInfoFileLocalFunc(backupData *history.BackupConfig, backupDir string) error {
	// Include local backup.
	canGetReport, err := checkBackupCanBeUsed(false, false, backupData)
	if err != nil {
		return err
	}
	if canGetReport {
		timestamp := backupData.Timestamp
		bckpDir, segPrefix, _, err := getBackupMasterDir(backupDir, backupData.BackupDir, backupData.DatabaseName)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableGetBackupPath("backup directory", timestamp, err))
			return err
		}
		gplog.Debug("%s", textmsg.InfoTextBackupDirPath(bckpDir))
		gplog.Debug("%s", textmsg.InfoTextSegmentPrefix(segPrefix))
		reportFile := gpbckpconfig.ReportFilePath(bckpDir, timestamp)
		// Sanitize the file path
		reportFile = filepath.Clean(reportFile)
		gplog.Debug("%s", textmsg.InfoTextCommandExecution("read file", reportFile))
		content, err := os.ReadFile(reportFile)
		if err != nil {
			gplog.Error("%s", textmsg.ErrorTextUnableGetBackupReport(backupData.Timestamp, err))
			return err
		}
		fmt.Println(string(content))
	}
	return nil
}

func execReportInfo(executablePath, reportInfoPluginCommand, pluginConfigFile, file string) (string, string, error) {
	cmd := execCommand(executablePath, reportInfoPluginCommand, pluginConfigFile, file)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
