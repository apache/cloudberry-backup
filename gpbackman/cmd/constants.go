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

const (
	commandName = "gpbackman"

	// Plugin commands.
	// To be able to work with various plugins,
	// it is highly desirable to use the commands from the plugin specification.
	// See https://github.com/greenplum-db/gpbackup/blob/710fe53305958c1faed2e6008b894b4923bed253/plugins/README.md
	deleteBackupPluginCommand = "delete_backup"
	restoreDataPluginCommand  = "restore_data"

	historyFileNameBaseConst = "gpbackup_history"
	historyFileDBSuffixConst = ".db"
	historyDBNameConst       = historyFileNameBaseConst + historyFileDBSuffixConst

	// Flags.
	historyDBFlagName            = "history-db"
	logFileFlagName              = "log-file"
	logLevelConsoleFlagName      = "log-level-console"
	logLevelFileFlagName         = "log-level-file"
	timestampFlagName            = "timestamp"
	pluginConfigFileFlagName     = "plugin-config"
	reportFilePluginPathFlagName = "plugin-report-file-path"
	deletedFlagName              = "deleted"
	failedFlagName               = "failed"
	cascadeFlagName              = "cascade"
	forceFlagName                = "force"
	olderThanDaysFlagName        = "older-than-days"
	beforeTimestampFlagName      = "before-timestamp"
	afterTimestampFlagName       = "after-timestamp"
	typeFlagName                 = "type"
	tableFlagName                = "table"
	schemaFlagName               = "schema"
	excludeFlagName              = "exclude"
	backupDirFlagName            = "backup-dir"
	parallelProcessesFlagName    = "parallel-processes"
	ignoreErrorsFlagName         = "ignore-errors"
	detailFlagName               = "detail"

	exitErrorCode = 1

	// Default for checking the existence of the file.
	checkFileExistsConst = true

	// Batch size for deleting from sqlite3.
	// This is to prevent problem with sqlite3.
	sqliteDeleteBatchSize = 1000
)

var (
	// Timestamp to delete all backups before.
	beforeTimestamp string
	// Timestamp to delete all backups after.
	afterTimestamp string
)
