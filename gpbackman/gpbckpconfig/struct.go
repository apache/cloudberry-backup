package gpbckpconfig

const (
	Layout     = "20060102150405"
	DateFormat = "Mon Jan 02 2006 15:04:05"
	// Backup types.
	BackupTypeFull         = "full"
	BackupTypeIncremental  = "incremental"
	BackupTypeDataOnly     = "data-only"
	BackupTypeMetadataOnly = "metadata-only"
	// Object filtering types.
	objectFilteringIncludeSchema = "include-schema"
	objectFilteringExcludeSchema = "exclude-schema"
	objectFilteringIncludeTable  = "include-table"
	objectFilteringExcludeTable  = "exclude-table"
	// Date deleted types.
	DateDeletedInProgress   = "In progress"
	DateDeletedPluginFailed = "Plugin Backup Delete Failed"
	DateDeletedLocalFailed  = "Local Delete Failed"
	// BackupS3Plugin S3 plugin names.
	BackupS3Plugin = "gpbackup_s3_plugin"
)
