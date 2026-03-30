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
