package gpbckpconfig

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-go-libs/operating"
)

// CheckTimestamp Returns error if timestamp is not valid.
func CheckTimestamp(timestamp string) error {
	timestampFormat := regexp.MustCompile(`^(\d{14})$`)
	if !timestampFormat.MatchString(timestamp) {
		return textmsg.ErrorValidationTimestamp()
	}
	return nil
}

func GetTimestampOlderThan(value uint) string {
	return time.Now().AddDate(0, 0, -int(value)).Format(Layout)
}

// CheckFullPath Returns error if path is not an absolute path or
// file does not exist.
func CheckFullPath(checkPath string, checkFileExists bool) error {
	if !filepath.IsAbs(checkPath) {
		return textmsg.ErrorValidationFullPath()
	}
	// In most cases this check should be mandatory.
	// But there are commands, that allows the history db file to be missing.
	if checkFileExists {
		if _, err := os.Stat(checkPath); errors.Is(err, os.ErrNotExist) {
			return textmsg.ErrorFileNotExist()
		}
	}
	return nil
}

// CheckTableFQN Returns error if table FQN is not in the format <schema.table>.
func CheckTableFQN(table string) error {
	format := regexp.MustCompile(`^.+\..+$`)
	if !format.MatchString(table) {
		return textmsg.ErrorValidationTableFQN()
	}
	return nil
}

// IsBackupActive Returns true if backup is active (not deleted).
func IsBackupActive(dateDeleted string) bool {
	return (dateDeleted == "" ||
		dateDeleted == DateDeletedPluginFailed ||
		dateDeleted == DateDeletedLocalFailed)
}

// IsPositiveValue Returns true if the value is positive.
func IsPositiveValue(value int) bool {
	return value > 0
}

// backupPluginCustomReportPath Returns custom report path:
//
//	<folder>/gpbackup_<YYYYMMDDHHMMSS>_report
func backupPluginCustomReportPath(timestamp, folderValue string) string {
	return filepath.Join("/", folderValue, ReportFileName(timestamp))
}

// backupS3PluginReportPath Returns path to report file name for gpbackup_s3_plugin plugin.
// Basic path for s3 plugin format:
//
//	<folder>/backups/<YYYYMMDD>/<YYYYMMDDHHMMSS>/gpbackup_<YYYYMMDDHHMMSS>_report
//
// See GetS3Path() func in https://github.com/greenplum-db/gpbackup-s3-plugin.
// If folder option is not specified or it is empty, the error will be returned.
func backupS3PluginReportPath(timestamp string, pluginOptions map[string]string) (string, error) {
	pathOption := "folder"
	// Timestamp validation is done on flags validation.
	// We assume, that is the correct value coming from.
	reportPathBasic := "backups/" + timestamp[0:8] + "/" + timestamp
	folderValue, exists := pluginOptions[pathOption]
	if !exists || folderValue == "" {
		return "", textmsg.ErrorValidationPluginOption(pathOption, BackupS3Plugin)
	}
	// It's necessary to return full path to report file with leading '/'.
	// But in config file folder value could be with leading '/' or without.
	// So we need to remove leading '/' and add it back.
	folderValue = strings.TrimPrefix(folderValue, "/")
	folderValue = strings.TrimSuffix(folderValue, "/")
	return filepath.Join("/", folderValue, reportPathBasic, ReportFileName(timestamp)), nil
}

// ReportFileName Returns report file name for specific timestamp.
// Report file name format: gpbackup_<YYYYMMDDHHMMSS>_report.
func ReportFileName(timestamp string) string {
	return "gpbackup_" + timestamp + "_report"
}

// CheckMasterBackupDir checks the backup directory for the master backup.
// It first tries to find the backup directory in the single-backup-dir format.
// If the single-backup-dir format is not used, it returns an error.
// If the single-backup-dir format is used, it returns the backup directory and sets the prefix to an empty string.
// If the single-backup-dir format is not found, it tries to find the backup directory with segment prefix format.
// If the backup directory with segment prefix format is not found, it returns an error.
// If multiple backup directories with segment prefix format are found, it returns an error.
// Otherwise, it returns the backup directory with segment prefix format, the segment prefix, and useSingleBackupDir flag to false.
func CheckMasterBackupDir(backupDir string) (string, string, bool, error) {
	// Try to find the backup directory in the single-backup-dir format.
	_, err := operating.System.Stat(fmt.Sprintf("%s/backups", backupDir))
	// The single-backup-dir directory format is not used.
	if err != nil && !os.IsNotExist(err) {
		return "", "", false, textmsg.ErrorFindBackupDirIn(backupDir, err)
	}
	if err == nil {
		// The single-backup-dir directory format is used, there's no prefix to parse.
		return backupDir, "", true, nil
	}
	// Try to find the backup directory with segment prefix format.
	backupDirForMaster, err := operating.System.Glob(fmt.Sprintf("%s/*-1/backups", backupDir))
	if err != nil {
		return "", "", false, textmsg.ErrorFindBackupDirIn(backupDir, err)
	}
	if len(backupDirForMaster) == 0 {
		return "", "", false, textmsg.ErrorNotFoundBackupDirIn(backupDir)
	}
	if len(backupDirForMaster) != 1 {
		return "", "", false, textmsg.ErrorSeveralFoundBackupDirIn(backupDir)
	}
	segPrefix := GetSegPrefix(backupDirForMaster[0])
	returnDir := filepath.Join(backupDir, fmt.Sprintf("%s-1", segPrefix))
	return returnDir, segPrefix, false, nil
}

// GetSegPrefix Returns segment prefix from the master backup directory.
func GetSegPrefix(backupDir string) string {
	indexOfBackupsSubstr := strings.LastIndex(backupDir, "-1/backups")
	_, segPrefix := path.Split(backupDir[:indexOfBackupsSubstr])
	return segPrefix
}

// ReportFilePath Returns path to report file.
func ReportFilePath(backupDir, timestamp string) string {
	return filepath.Join(BackupDirPath(backupDir, timestamp), ReportFileName(timestamp))
}

// BackupDirPath Returns path to full backup directory.
func BackupDirPath(backupDir, timestamp string) string {
	return filepath.Join(backupDir, "backups", timestamp[0:8], timestamp)
}


