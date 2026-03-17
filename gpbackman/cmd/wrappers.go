package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/spf13/pflag"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-backup/history"
)

var execOSExit = os.Exit

func logHeadersDebug() {
	gplog.Debug("Start %s version %s", commandName, getVersion())
	gplog.Debug("Use console log level: %s", rootLogLevelConsole)
	gplog.Debug("Use file log level: %s", rootLogLevelFile)
	gplog.Debug("%s command: %s", commandName, os.Args)
}

// Sets the log levels for the console and file loggers.
// Uppercase or lowercase letters are accepted.
// If an incorrect value is specified, an error is returned.
func setLogLevelConsole(level string) error {
	switch strings.ToLower(level) {
	case "info":
		gplog.SetVerbosity(gplog.LOGINFO)
	case "error":
		gplog.SetVerbosity(gplog.LOGERROR)
	case "debug":
		gplog.SetVerbosity(gplog.LOGDEBUG)
	case "verbose":
		gplog.SetVerbosity(gplog.LOGVERBOSE)
	default:
		return textmsg.ErrorInvalidValueError()
	}
	return nil
}

// Sets the log levels for the console and file loggers.
// Uppercase or lowercase letters are accepted.
// If an incorrect value is specified, an error is returned.
func setLogLevelFile(level string) error {
	switch strings.ToLower(level) {
	case "info":
		gplog.SetLogFileVerbosity(gplog.LOGINFO)
	case "error":
		gplog.SetLogFileVerbosity(gplog.LOGERROR)
	case "debug":
		gplog.SetLogFileVerbosity(gplog.LOGDEBUG)
	case "verbose":
		gplog.SetLogFileVerbosity(gplog.LOGVERBOSE)
	default:
		return textmsg.ErrorInvalidValueError()
	}
	return nil
}

func getHistoryDBPath(historyDBPath string) string {
	var historyDBName = historyDBNameConst
	if historyDBPath != "" {
		return historyDBPath
	}
	return historyDBName
}

func checkCompatibleFlags(flags *pflag.FlagSet, flagNames ...string) error {
	n := 0
	for _, name := range flagNames {
		if flags.Changed(name) {
			n++
		}
	}
	if n > 1 {
		return textmsg.ErrorIncompatibleFlagsError()
	}
	return nil
}

func formatBackupDuration(value float64) string {
	hours := int(value / 3600)
	minutes := (int(value) % 3600) / 60
	seconds := int(value) % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

// The backup can be used in one of the cases for local and plugin backups:
// - backup is active
// - backup is not active, but the --force flag is set.
// Returns:
// - true, if backup can be used;
// - false, if backup can't be used.
// Errors and warnings will also returned and logged.
func checkBackupCanBeUsed(deleteForce, skipLocalBackup bool, backupData *history.BackupConfig) (bool, error) {
	result := false
	err := checkLocalBackupStatus(skipLocalBackup, gpbckpconfig.IsLocal(backupData))
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableWorkBackup(backupData.Timestamp, err))
		return result, err
	}
	if gpbckpconfig.IsInProgress(backupData) && !deleteForce {
		gplog.Error("%s", textmsg.InfoTextBackupStatus(backupData.Timestamp, backupData.Status))
		return result, nil
	}
	backupDateDeleted, errDateDeleted := gpbckpconfig.GetBackupDateDeleted(backupData)
	if errDateDeleted != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableGetBackupValue("date deletion", backupData.Timestamp, errDateDeleted))
	}
	// If the backup date deletion has invalid value, try to delete the backup.
	if gpbckpconfig.IsBackupActive(backupDateDeleted) || errDateDeleted != nil {
		result = true
	} else {
		if backupDateDeleted == gpbckpconfig.DateDeletedInProgress {
			// We do not return the error here,
			// because it is necessary to leave the possibility of starting the process
			// of deleting backups that are stuck in the "In Progress" status using the --force flag.
			gplog.Error("%s", textmsg.ErrorTextBackupDeleteInProgress(backupData.Timestamp, textmsg.ErrorBackupDeleteInProgressError()))
		} else {
			gplog.Debug("%s", textmsg.InfoTextBackupAlreadyDeleted(backupData.Timestamp))
		}
	}
	// If flag --force is set.
	if deleteForce {
		result = true
	}
	return result, nil
}

// Check that specified backup type is supported.
func checkBackupType(backupType string) error {
	var validVType = map[string]bool{
		gpbckpconfig.BackupTypeFull:         true,
		gpbckpconfig.BackupTypeIncremental:  true,
		gpbckpconfig.BackupTypeMetadataOnly: true,
		gpbckpconfig.BackupTypeDataOnly:     true,
	}
	if !validVType[backupType] {
		return textmsg.ErrorInvalidValueError()
	}
	return nil
}

// Check skip flag and local backup status.
// SkipLocalBackup - true, local backup - true, returns "is a local backup" error.
// SkipLocalBackup - false,local backup - false, returns "is not a local backup" error.
func checkLocalBackupStatus(skipLocalBackup, isLocalBackup bool) error {
	if skipLocalBackup && isLocalBackup {
		return textmsg.ErrorBackupLocalStorageError()
	}
	if !skipLocalBackup && !isLocalBackup {
		return textmsg.ErrorBackupNotLocalStorageError()
	}
	return nil
}

func getBackupMasterDir(backupDir, backupDataBackupDir, backupDataDBName string) (string, string, bool, error) {
	if backupDir != "" {
		return gpbckpconfig.CheckMasterBackupDir(backupDir)
	}
	if backupDataBackupDir != "" {
		return gpbckpconfig.CheckMasterBackupDir(backupDataBackupDir)
	}
	// Try to get the backup directory from the cluster configuration.
	// If the script executed not on the master host, the backup directory will not be found.
	// And we return "value not set" error.
	backupDirClusterInfo := getBackupMasterDirClusterInfo(backupDataDBName)
	if backupDirClusterInfo != "" {
		return backupDirClusterInfo, gpbckpconfig.GetSegPrefix(filepath.Join(backupDirClusterInfo, "backups")), false, nil
	}
	return "", "", false, textmsg.ErrorValidationValue()
}

func getBackupSegmentDir(backupDir, backupDataBackupDir, backupDataDir, segPrefix, segID string, isSingleBackupDir bool) (string, error) {
	if backupDir != "" {
		return checkSingleBackupDir(backupDir, segPrefix, segID, isSingleBackupDir), nil
	}
	if backupDataBackupDir != "" {
		return checkSingleBackupDir(backupDataBackupDir, segPrefix, segID, isSingleBackupDir), nil
	}
	if backupDataDir != "" {
		return backupDataDir, nil
	}
	return "", textmsg.ErrorValidationValue()
}

func checkSingleBackupDir(backupDir, segPrefix, segID string, isSingleBackupDir bool) string {
	if isSingleBackupDir {
		return backupDir
	}
	return filepath.Join(backupDir, fmt.Sprintf("%s%s", segPrefix, segID))
}

func getBackupMasterDirClusterInfo(dbName string) string {
	db, err := gpbckpconfig.NewClusterLocalClusterConn(dbName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableConnectLocalCluster(err))
		return ""
	}
	defer db.Close()
	sqlQuery := "SELECT datadir FROM gp_segment_configuration WHERE content = -1 AND role = 'p';"
	queryResult, err := gpbckpconfig.ExecuteQueryLocalClusterConn[string](db, sqlQuery)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableGetBackupDirLocalClusterConn(err))
		return ""
	}
	gplog.Debug("Master data directory: %s", queryResult)
	return queryResult
}

func getSegmentConfigurationClusterInfo(dbName string) ([]gpbckpconfig.SegmentConfig, error) {
	queryResult := make([]gpbckpconfig.SegmentConfig, 0)
	db, err := gpbckpconfig.NewClusterLocalClusterConn(dbName)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableConnectLocalCluster(err))
		return queryResult, err
	}
	defer db.Close()
	sqlQuery := "SELECT content as contentid, hostname, datadir FROM gp_segment_configuration WHERE role = 'p' and content != -1 ORDER BY content;"
	queryResult, err = gpbckpconfig.ExecuteQueryLocalClusterConn[[]gpbckpconfig.SegmentConfig](db, sqlQuery)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableGetBackupDirLocalClusterConn(err))
		return queryResult, err
	}
	return queryResult, nil
}

func handleErrorDB(backupName, errorMessage, backupStatus string, hDB *sql.DB) {
	gplog.Error("%s", errorMessage)
	err := gpbckpconfig.UpdateDeleteStatus(backupName, backupStatus, hDB)
	if err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableSetBackupStatus(backupStatus, backupName, err))
	}
}
