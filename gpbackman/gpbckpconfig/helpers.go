package gpbckpconfig

import (
	"errors"
	"strings"
	"time"

	"github.com/apache/cloudberry-backup/history"
	"github.com/apache/cloudberry-backup/utils"
)

// GetBackupType Get backup type.
// The value is calculated, based on:
//   - full - contains user data, all global and local metadata for the database;
//   - incremental – contains user data, all global and local metadata changed since a previous full backup;
//   - metadata-only – contains only global and local metadata for the database;
//   - data-only – contains only user data from the database.
//
// In all other cases, an error is returned.
func GetBackupType(backupConfig *history.BackupConfig) (string, error) {
	// For gpbackup you cannot combine --data-only or --metadata-only with --incremental (see docs).
	// So these options cannot be set at the same time.
	// If not one of the --data-only, --metadata-only and --incremental flags is not set,
	// the full value is returned.
	// But if there are no tables in backup set contain data,
	// the metadata-only value is returned.
	// See https://github.com/woblerr/gpbackup/blob/b061a47b673238439442340e66ca57d896edacd5/backup/backup.go#L127-L129
	switch {
	case !backupConfig.Incremental && !backupConfig.DataOnly && !backupConfig.MetadataOnly:
		return BackupTypeFull, nil
	case backupConfig.Incremental && !backupConfig.DataOnly && !backupConfig.MetadataOnly:
		return BackupTypeIncremental, nil
	case backupConfig.DataOnly && !backupConfig.Incremental && !backupConfig.MetadataOnly:
		return BackupTypeDataOnly, nil
	// If only metadata-only value.
	// Or combination metadata-only and incremental or metadata-only and data-only.
	// The case when there are no tables in backup set contain data.
	case (backupConfig.MetadataOnly && !backupConfig.Incremental) || (backupConfig.MetadataOnly && !backupConfig.DataOnly):
		return BackupTypeMetadataOnly, nil
	default:
		return "", errors.New("backup type does not match any of the available values")
	}
}

// GetObjectFilteringInfo Get object filtering information.
// The value is calculated, base on whether at least one of the flags was specified:
//   - include-schema – at least one "--include-schema" option was specified;
//   - exclude-schema – at least one "--exclude-schema" option was specified;
//   - include-table – at least one "--include-table" option was specified;
//   - exclude-table – at least one "--exclude-table" option was specified;
//   - "" - no options was specified.
//
// For gpbackup only one type of filters can be used (see docs).
// So these options cannot be set at the same time.
// If not one of these flags is not set,
// the "" value is returned.
// In all other cases, an error is returned.
func GetObjectFilteringInfo(backupConfig *history.BackupConfig) (string, error) {
	switch {
	case backupConfig.IncludeSchemaFiltered &&
		!backupConfig.ExcludeSchemaFiltered &&
		!backupConfig.IncludeTableFiltered &&
		!backupConfig.ExcludeTableFiltered:
		return objectFilteringIncludeSchema, nil
	case backupConfig.ExcludeSchemaFiltered &&
		!backupConfig.IncludeSchemaFiltered &&
		!backupConfig.IncludeTableFiltered &&
		!backupConfig.ExcludeTableFiltered:
		return objectFilteringExcludeSchema, nil
	case backupConfig.IncludeTableFiltered &&
		!backupConfig.IncludeSchemaFiltered &&
		!backupConfig.ExcludeSchemaFiltered &&
		!backupConfig.ExcludeTableFiltered:
		return objectFilteringIncludeTable, nil
	case backupConfig.ExcludeTableFiltered &&
		!backupConfig.IncludeSchemaFiltered &&
		!backupConfig.ExcludeSchemaFiltered &&
		!backupConfig.IncludeTableFiltered:
		return objectFilteringExcludeTable, nil
	case !backupConfig.ExcludeTableFiltered &&
		!backupConfig.IncludeSchemaFiltered &&
		!backupConfig.ExcludeSchemaFiltered &&
		!backupConfig.IncludeTableFiltered:
		return "", nil
	default:
		return "", errors.New("backup filtering type does not match any of the available values")
	}
}

// GetObjectFilteringDetails returns a comma-separated string with object filtering details
// depending on the active filtering type. If no filtering is active, it returns an empty string.
func GetObjectFilteringDetails(backupConfig *history.BackupConfig) string {
	filter, _ := GetObjectFilteringInfo(backupConfig)
	switch filter {
	case objectFilteringIncludeTable:
		return strings.Join(backupConfig.IncludeRelations, ", ")
	case objectFilteringExcludeTable:
		return strings.Join(backupConfig.ExcludeRelations, ", ")
	case objectFilteringIncludeSchema:
		return strings.Join(backupConfig.IncludeSchemas, ", ")
	case objectFilteringExcludeSchema:
		return strings.Join(backupConfig.ExcludeSchemas, ", ")
	default:
		return ""
	}
}

// GetBackupDate Get backup date.
// If an error occurs when parsing the date, the empty string and error are returned.
func GetBackupDate(backupConfig *history.BackupConfig) (string, error) {
	var date string
	t, err := time.Parse(Layout, backupConfig.Timestamp)
	if err != nil {
		return date, err
	}
	date = t.Format(DateFormat)
	return date, nil
}

// GetBackupDuration Get backup duration in seconds.
// If an error occurs when parsing the date, the zero duration and error are returned.
func GetBackupDuration(backupConfig *history.BackupConfig) (float64, error) {
	var zeroDuration float64
	startTime, err := time.Parse(Layout, backupConfig.Timestamp)
	if err != nil {
		return zeroDuration, err
	}
	endTime, err := time.Parse(Layout, backupConfig.EndTime)
	if err != nil {
		return zeroDuration, err
	}
	return endTime.Sub(startTime).Seconds(), nil
}

// GetBackupDateDeleted Get backup deletion date or backup deletion status.
// The possible values are:
//   - In progress - if the value is set to "In progress";
//   - Plugin Backup Delete Failed - if the value is set to "Plugin Backup Delete Failed";
//   - Local Delete Failed - if the value is set to "Local Delete Failed";
//   - "" - if backup is active;
//   - date  in format "Mon Jan 02 2006 15:04:05" - if backup is deleted and deletion timestamp is set.
//
// In all other cases, an error is returned.
func GetBackupDateDeleted(backupConfig *history.BackupConfig) (string, error) {
	switch backupConfig.DateDeleted {
	case "", DateDeletedInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed:
		return backupConfig.DateDeleted, nil
	default:
		t, err := time.Parse(Layout, backupConfig.DateDeleted)
		if err != nil {
			return backupConfig.DateDeleted, err
		}
		return t.Format(DateFormat), nil
	}
}

// IsSuccess Check backup status.
// Returns:
//   - true  - if backup is successful,
//   - false - false if backup is not successful or in progress.
//
// In all other cases, an error is returned.
func IsSuccess(backupConfig *history.BackupConfig) (bool, error) {
	switch backupConfig.Status {
	case history.BackupStatusSucceed:
		return true, nil
	case history.BackupStatusFailed, history.BackupStatusInProgress:
		return false, nil
	default:
		return false, errors.New("backup status does not match any of the available values")
	}
}

// IsLocal Check if the backup in local or in plugin storage.
// Returns:
//   - true  - if the backup in local storage (plugin field is empty);
//   - false - if the backup in plugin storage (plugin field is not empty).
func IsLocal(backupConfig *history.BackupConfig) bool {
	return backupConfig.Plugin == ""
}

// IsInProgress Check if the backup is in progress.
func IsInProgress(backupConfig *history.BackupConfig) bool {
	return backupConfig.Status == history.BackupStatusInProgress
}

// GetReportFilePathPlugin Return path to report file name for specific plugin.
// If custom report path is set, it is returned.
// Otherwise, the path from plugin is returned.
func GetReportFilePathPlugin(backupConfig *history.BackupConfig, customReportPath string, pluginOptions map[string]string) (string, error) {
	if customReportPath != "" {
		return backupPluginCustomReportPath(backupConfig.Timestamp, customReportPath), nil
	}
	// In future another plugins may be added.
	switch backupConfig.Plugin {
	case BackupS3Plugin:
		return backupS3PluginReportPath(backupConfig.Timestamp, pluginOptions)
	default:
		// nothing to do
	}
	return "", errors.New("the path to the report is not specified")
}

// CheckObjectFilteringExists checks if the object filtering exists in the backup.
//
// This function is responsible for determining whether table or schema filtering exists in the backup, and if so, whether the specified filter type is being used.
// Returns:
//   - true - if table or schema filtering exists in the backup or no filters are specified;
//   - false - if table or schema filtering does not exists in the backup.
func CheckObjectFilteringExists(backupConfig *history.BackupConfig, tableFilter, schemaFilter, objectFilter string, excludeFilter bool) bool {
	switch {
	case tableFilter != "" && !excludeFilter:
		if objectFilter == objectFilteringIncludeTable {
			return utils.Exists(backupConfig.IncludeRelations, tableFilter)
		}
		return false
	case tableFilter != "" && excludeFilter:
		if objectFilter == objectFilteringExcludeTable {
			return utils.Exists(backupConfig.ExcludeRelations, tableFilter)
		}
		return false
	case schemaFilter != "" && !excludeFilter:
		if objectFilter == objectFilteringIncludeSchema {
			return utils.Exists(backupConfig.IncludeSchemas, schemaFilter)
		}
		return false
	case schemaFilter != "" && excludeFilter:
		if objectFilter == objectFilteringExcludeSchema {
			return utils.Exists(backupConfig.ExcludeSchemas, schemaFilter)
		}
		return false
	default:
		return true
	}
}
