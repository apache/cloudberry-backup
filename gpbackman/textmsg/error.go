package textmsg

import (
	"errors"
	"fmt"
	"strings"
)

// Collection of possible error texts.

// Errors that occur when working with a history db.

func ErrorTextUnableActionHistoryDB(value string, err error) string {
	return fmt.Sprintf("Unable to %s history db. Error: %v", value, err)
}

func ErrorTextUnableReadHistoryDB(err error) string {
	return fmt.Sprintf("Unable to read data from history db. Error: %v", err)
}

// Errors that occur when working with a backup data.

func ErrorTextUnableGetBackupInfo(backupName string, err error) string {
	return fmt.Sprintf("Unable to get info for backup %s. Error: %v", backupName, err)
}

func ErrorTextUnableGetBackupValue(value, backupName string, err error) string {
	return fmt.Sprintf("Unable to get backup %s for backup %s. Error: %v", value, backupName, err)
}

func ErrorTextUnableSetBackupStatus(value, backupName string, err error) string {
	return fmt.Sprintf("Unable to set %s status for backup %s. Error: %v", value, backupName, err)
}

func ErrorTextUnableDeleteBackup(backupName string, err error) string {
	return fmt.Sprintf("Unable to delete backup %s. Error: %v", backupName, err)
}

func ErrorTextUnableWorkBackup(backupName string, err error) string {
	return fmt.Sprintf("Unable to work with backup %s. Error: %v", backupName, err)
}

func ErrorTextUnableDeleteBackupCascade(backupName string, err error) string {
	return fmt.Sprintf("Unable to delete dependent backups for backup %s. Error: %v", backupName, err)
}

func ErrorTextUnableDeleteBackupUseCascade(backupName string, err error) string {
	return fmt.Sprintf("Backup %s has dependent backups. Use --cascade option. Error: %v", backupName, err)
}

func ErrorTextBackupDeleteInProgress(backupName string, err error) string {
	return fmt.Sprintf("Backup %s deletion in progress. Error: %v", backupName, err)
}

func ErrorTextUnableGetBackupReport(backupName string, err error) string {
	return fmt.Sprintf("Unable to get report for the backup %s. Error: %v", backupName, err)
}

func ErrorTextUnableGetBackupPath(value, backupName string, err error) string {
	return fmt.Sprintf("Unable to get path to %s for the backup %s. Error: %v", value, backupName, err)
}

// Errors that occur when working with a backup plugin.

func ErrorTextUnableReadPluginConfigFile(err error) string {
	return fmt.Sprintf("Unable to read plugin config file. Error: %v", err)
}

// Error that occur when working with a local backup.

func ErrorTextCommandExecutionFailed(err error, values ...string) string {
	return fmt.Sprintf("Command failed: %s. Error: %v", strings.Join(values, " "), err)
}

// Errors that occur during flags validation.

func ErrorTextUnableValidateFlag(value, flag string, err error) string {
	return fmt.Sprintf(
		"Unable to validate value %s for flag %s. Error: %v", value, flag, err)
}

func ErrorTextUnableCompatibleFlags(err error, values ...string) string {
	return fmt.Sprintf(
		"Unable to use the following flags together: %s. Error: %v",
		strings.Join(values, ", "), err)
}

func ErrorTextUnableValidateValue(err error, values ...string) string {
	return fmt.Sprintf("Unable to validate provided arguments. Try to use one of flags: %s. Error: %v",
		strings.Join(values, ", "), err)
}

// Errors that occur when working with a local cluster.

func ErrorTextUnableConnectLocalCluster(err error) string {
	return fmt.Sprintf("Unable to connect to the cluster locally. Error: %v", err)
}

func ErrorTextUnableGetBackupDirLocalClusterConn(err error) string {
	return fmt.Sprintf("Unable to get backup directory from a local connection to the cluster. Error: %v", err)
}

// Errors that occur when working with backup reports.

func ErrorTextUnableGetReport(err error) string {
	return fmt.Sprintf("Unable to get report. Error: %v", err)
}

func ErrorTextUnableCheckTimestamp(err error) string {
	return fmt.Sprintf("Unable to check timestamp. Error: %v", err)
}

func ErrorTextUnableGetSegPrefix(err error) string {
	return fmt.Sprintf("Unable to get segment prefix. Error: %v", err)
}

func ErrorTextUnableCheckPath(err error) string {
	return fmt.Sprintf("Unable to check path. Error: %v", err)
}

// Errors that occur when working with local backup directories.

func ErrorTextUnableDeleteLocalBackup(err error) string {
	return fmt.Sprintf("Unable to delete local backup. Error: %v", err)
}

func ErrorTextUnableCleanDB(err error) string {
	return fmt.Sprintf("Unable to clean db. Error: %v", err)
}

func ErrorTextUnableDeletePluginBackup(backupName string, err error) string {
	return fmt.Sprintf("Unable to delete plugin backup %s. Error: %v", backupName, err)
}

func ErrorTextUnableDeletePluginReport(backupName string, err error) string {
	return fmt.Sprintf("Unable to delete plugin report %s. Error: %v", backupName, err)
}

func ErrorTextUnableUpdateDeleteStatus(backupName string, err error) string {
	return fmt.Sprintf("Unable to update delete status for %s. Error: %v", backupName, err)
}

func ErrorTextUnableCheckBackupDir(backupName string, err error) string {
	return fmt.Sprintf("Unable to check backup dir %s. Error: %v", backupName, err)
}

func ErrorTextUnableDeleteFile(value1, value2 string, err error) string {
	return fmt.Sprintf("Unable to delete %s %s. Error: %v", value1, value2, err)
}

func InfoTextSetBackupDeleteStatus(backupName, status string) string {
	return fmt.Sprintf("Set backup %s delete status to %s", backupName, status)
}

// Error that is returned when flags validation not passed.

func ErrorInvalidValueError() error {
	return errors.New("invalid flag value")
}

func ErrorIncompatibleFlagsError() error {
	return errors.New("incompatible flags")
}

func ErrorNotIndependentFlagsError() error {
	return errors.New("not an independent flag")
}

// Error that is returned when backup deletion fails.

func ErrorBackupDeleteInProgressError() error {
	return errors.New("backup deletion in progress")
}

func ErrorBackupDeleteCascadeOptionError() error {
	return errors.New("use cascade option")
}

func ErrorBackupLocalStorageError() error {
	return errors.New("is a local backup")
}

func ErrorBackupNotLocalStorageError() error {
	return errors.New("is not a local backup")
}

// Error that is returned when some validation fails.

func ErrorValidationFullPath() error {
	return errors.New("not an absolute path")
}

func ErrorFileNotExist() error {
	return errors.New("file not exist")
}

func ErrorValidationTableFQN() error {
	return errors.New("not a fully qualified table name")
}

func ErrorValidationTimestamp() error {
	return errors.New("not a timestamp")
}

func ErrorValidationValue() error {
	return errors.New("value not set")
}

func ErrorEmptyDatabase() error {
	return errors.New("database name cannot be empty")
}

// Error that is returned when some plugin options validation fails.

func ErrorValidationPluginOption(value, pluginName string) error {
	return fmt.Errorf("invalid plugin %s option value for plugin %s", value, pluginName)
}

// Errors that are returned when some backup directory validation fails.

func ErrorFindBackupDirIn(value string, err error) error {
	return fmt.Errorf("can not find backup directory in %s, error: %v", value, err.Error())
}

func ErrorNotFoundBackupDirIn(value string) error {
	return fmt.Errorf("no backup directory found in %s", value)
}

func ErrorSeveralFoundBackupDirIn(value string) error {
	return fmt.Errorf("several backup directory found in %s", value)
}

// Error that is returned when backup not found.

func ErrorBackupNotFoundError(backupName string) error {
	return fmt.Errorf("backup %s not found", backupName)
}

func ErrorInvalidInputValueError(value string) error {
	return fmt.Errorf("invalid input value: %s", value)
}

// Error that is returned when backup has specific delete status.

func ErrorSetBackupDeleteStatus(backupName, status string) error {
	return fmt.Errorf("backup %s has delete status %s", backupName, status)
}

// Error that is returned when backup dir is not specified.

func ErrorBackupDirNotSpecifiedError() error {
	return errors.New("backup dir is not specified")
}
