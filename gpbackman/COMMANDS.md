<!--
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
-->

- [Delete all existing backups older than the specified time condition (`backup-clean`)](#delete-all-existing-backups-older-than-the-specified-time-condition-backup-clean)
  - [Examples](#examples)
    - [Delete all backups from local storage older than the specified time condition](#delete-all-backups-from-local-storage-older-than-the-specified-time-condition)
    - [Delete all backups using storage plugin older than n days](#delete-all-backups-using-storage-plugin-older-than-n-days)
- [Delete a specific existing backup (`backup-delete`)](#delete-a-specific-existing-backup-backup-delete)
  - [Examples](#examples-1)
    - [Delete existing backup from local storage](#delete-existing-backup-from-local-storage)
    - [Delete existing backup using storage plugin](#delete-existing-backup-using-storage-plugin)
- [Display information about backups (`backup-info`)](#display-information-about-backups-backup-info)
  - [Examples](#examples-2)
- [Clean deleted backups from the history database (`history-clean`)](#clean-deleted-backups-from-the-history-database-history-clean)
  - [Examples](#examples-3)
    - [Delete information about deleted backups from history database older than n days](#delete-information-about-deleted-backups-from-history-database-older-than-n-days)
    - [Delete information about deleted backups from history database older than timestamp](#delete-information-about-deleted-backups-from-history-database-older-than-timestamp)
- [Display the report for a specific backup (`report-info`)](#display-the-report-for-a-specific-backup-report-info)
  - [Examples](#examples-4)
    - [Display the backup report from local storage](#display-the-backup-report-from-local-storage)
    - [Display the backup report using storage plugin](#display-the-backup-report-using-storage-plugin)

# Delete all existing backups older than the specified time condition (`backup-clean`)

Available options for `backup-clean` command and their description:
```bash
./gpbackman backup-clean -h
elete all existing backups older than the specified time condition.

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
If the --history-db option is not specified, the history database will be searched in the current directory.

Usage:
  gpbackman backup-clean [flags]

Flags:
      --after-timestamp string    delete backup sets newer than the given timestamp
      --backup-dir string         the full path to backup directory for local backups
      --before-timestamp string   delete backup sets older than the given timestamp
      --cascade                   delete all dependent backups
  -h, --help                      help for backup-clean
      --older-than-days uint      delete backup sets older than the given number of days
      --parallel-processes int    the number of parallel processes to delete local backups (default 1)
      --plugin-config string      the full path to plugin config file

Global Flags:
      --history-db string          full path to the gpbackup_history.db file
      --log-file string            full path to log file directory, if not specified, the log file will be created in the $HOME/gpAdminLogs directory
      --log-level-console string   level for console logging (error, info, debug, verbose) (default "info")
      --log-level-file string      level for file logging (error, info, debug, verbose) (default "info")
```

## Examples
### Delete all backups from local storage older than the specified time condition

Delete specific backup :
```bash
./gpbackman backup-clean \
  --before-timestamp 20240701100000 \
  --cascade
```

Delete specific backup with specifying the number of parallel processes:
```bash
./gpbackman backup-delete \
  --older-than-days 7 \
  --parallel-processes 5
```

### Delete all backups using storage plugin older than n days
Delete all backups older than 7 days and all dependent backups:
```bash
./gpbackman backup-clean \
  --older-than-days 7 \
  --plugin-config /tmp/gpbackup_plugin_config.yaml \
  --cascade
```

# Delete a specific existing backup (`backup-delete`)

Available options for `backup-delete` command and their description:

```bash
./gpbackman backup-delete -h
Delete a specific existing backup.

The --timestamp option must be specified. It could be specified multiple times.

By default, the existence of dependent backups is checked and deletion process is not performed,
unless the --cascade option is passed in.

If backup already deleted, the deletion process is skipped, unless --force option is specified.
If errors occur during the deletion process, the errors can be ignored using the --ignore-errors option.
The --ignore-errors option can be used only with --force option.

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
If the --history-db option is not specified, the history database will be searched in the current directory.

Usage:
  gpbackman backup-delete [flags]

Flags:
      --backup-dir string        the full path to backup directory for local backups
      --cascade                  delete all dependent backups for the specified backup timestamp
      --force                    try to delete, even if the backup already mark as deleted
  -h, --help                     help for backup-delete
      --ignore-errors            ignore errors when deleting backups
      --parallel-processes int   the number of parallel processes to delete local backups (default 1)
      --plugin-config string     the full path to plugin config file
      --timestamp stringArray    the backup timestamp for deleting, could be specified multiple times

Global Flags:
      --history-db string          full path to the gpbackup_history.db file
      --log-file string            full path to log file directory, if not specified, the log file will be created in the $HOME/gpAdminLogs directory
      --log-level-console string   level for console logging (error, info, debug, verbose) (default "info")
      --log-level-file string      level for file logging (error, info, debug, verbose) (default "info")
```

## Examples
### Delete existing backup from local storage
Delete specific backup with specifying directory path:
```bash
./gpbackman backup-delete \
  --timestamp 20230809232817 \
  --backup-dir /some/path
```

Delete specific backup with specifying the number of parallel processes:
```bash
./gpbackman backup-delete \
  --timestamp 20230809212220 \
  --parallel-processes 5
```

### Delete existing backup using storage plugin
Delete specific backup:
```bash
./gpbackman backup-delete \
  --timestamp 20230725101959 \
  --plugin-config /tmp/gpbackup_plugin_config.yaml
```

Delete specific backup and all dependent backups:
```bash
./gpbackman backup-delete \
  --timestamp 20230725101115 \
  --plugin-config /tmp/gpbackup_plugin_config.yaml \
  --cascade
```

# Display information about backups (`backup-info`)

Available options for `backup-info` command and their description:

```bash
./gpbackman backup-info -h
Display information about backups.

By default, only active backups or backups with deletion status "In progress" from gpbackup_history.db are displayed.

To display deleted backups, use the --deleted option.
To display failed backups, use the --failed option.
To display all backups, use --deleted and --failed options together.

To display backups of a specific type, use the --type option.

To display backups that include the specified table, use the --table option. 
The formatting rules for <schema>.<table> match those of the --include-table option in gpbackup.

To display backups that include the specified schema, use the --schema option. 
The formatting rules for <schema> match those of the --include-schema option in gpbackup.

To display backups that exclude the specified table, use the --table and --exclude options. 
The formatting rules for <schema>.<table> match those of the --exclude-table option in gpbackup.

To display backups that exclude the specified schema, use the --schema and --exclude options. 
The formatting rules for <schema> match those of the --exclude-schema option in gpbackup.

To display details about object filtering, use the --detail option.
The details are presented as follows, depending on the active filtering type:
  * include-table / exclude-table: a comma-separated list of fully-qualified table names in the format <schema>.<table>;
  * include-schema / exclude-schema: a comma-separated list of schema names;
  * if no object filtering was used, the value is empty.

To display a backup chain for a specific backup, use the --timestamp option.
In this mode, the backup with the specified timestamp and all of its dependent backups will be displayed.
The deleted and failed backups are always included in this mode.
To display object filtering details in this mode, use the --detail option.
When --timestamp is set, the following options cannot be used: --type, --table, --schema, --exclude, --failed, --deleted.

To display the "object filtering details" column for all backups without using --timestamp, use the --detail option.

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database will be searched in the current directory.

Usage:
  gpbackman backup-info [flags]

Flags:
      --deleted            show deleted backups
      --detail             show object filtering details
      --exclude            show backups that exclude the specific table (format <schema>.<table>) or schema
      --failed             show failed backups
  -h, --help               help for backup-info
      --schema string      show backups that include the specified schema
      --table string       show backups that include the specified table (format <schema>.<table>)
      --timestamp string   show backup info and its dependent backups for the specified timestamp
      --type string        backup type filter (full, incremental, data-only, metadata-only)

Global Flags:
      --history-db string          full path to the gpbackup_history.db file
      --log-file string            full path to log file directory, if not specified, the log file will be created in the $HOME/gpAdminLogs directory
      --log-level-console string   level for console logging (error, info, debug, verbose) (default "info")
      --log-level-file string      level for file logging (error, info, debug, verbose) (default "info")
```

The following information is provided about each backup:
* `TIMESTAMP` - backup name, timestamp (`YYYYMMDDHHMMSS`) when the backup was taken;
* `DATE`- date in format `Mon Jan 02 2006 15:04:05` when the backup was taken;
* `STATUS`- backup status: `Success` or `Failure`; 
* `DATABASE` - database name for which the backup was performed	(specified by `--dbname` option on the `gpbackup` command).
* `TYPE` - backup type:
    - `full` - contains user data, all global and local metadata for the database;
    - `incremental` – contains user data, all global and local metadata changed since a previous full backup;
    - `metadata-only` – contains only global and local metadata for the database;
    - `data-only` – contains only user data from the database.

* `OBJECT FILTERING` - whether the object filtering options were used when executing the `gpbackup` command:
    - `include-schema` – at least one `--include-schema` option was specified;
    - `exclude-schema` – at least one `--exclude-schema` option was specified;
    - `include-table` – at least one `--include-table` option was specified;
    - `exclude-table` – at least one `--exclude-table` option was specified;
    - `""` - no options was specified.

* `PLUGIN` - plugin name that was used to configure the backup destination;
* `DURATION` -  backup duration in the format `hh:mm:ss`;
* `DATE DELETED` - backup deletion status:
    - `In progress` - the deletion is in progress;
    - `Plugin Backup Delete Failed` - last delete attempt failed to delete backup from plugin storage;
    - `Local Delete Failed` - last delete attempt failed to delete backup from local storage.;
    - `""` - if backup is active;
    - date  in format `Mon Jan 02 2006 15:04:05` - if backup is deleted and deletion timestamp is set.

If the `--detail` option is specified, the following additional information is provided:
* `OBJECT FILTERING DETAILS` - details about object filtering:
    - if `include-table` or `exclude-table` filtering was used, a comma-separated list of fully-qualified table names in the format `<schema>.<table>`;
    - if `include-schema` or `exclude-schema` filtering was used, a comma-separated list of schema names;
    - if no object filtering was used, the value is empty.

If gpbackup is launched without specifying `--metadata-only` flag, but there were no tables that contain data for backup, then gpbackup will only perform a `metadata-only` backup. The logs will contain messages like `No tables in backup set contain data. Performing metadata-only backup instead.` As a result, gpBackMan will display such backups as `metadata-only`.

## Examples

Display info for active backups from `gpbackup_history.db`:
```bash
./gpbackman backup-info

 TIMESTAMP      | DATE                     | STATUS  | DATABASE | TYPE          | OBJECT FILTERING | PLUGIN             | DURATION | DATE DELETED                
----------------+--------------------------+---------+----------+---------------+------------------+--------------------+----------+-----------------------------
 20230809232817 | Wed Aug 09 2023 23:28:17 | Success | demo     | full          |                  |                    | 04:00:03 |                             
 20230725110051 | Tue Jul 25 2023 11:00:51 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:20 |                             
 20230725102950 | Tue Jul 25 2023 10:29:50 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:19 |                             
 20230725102831 | Tue Jul 25 2023 10:28:31 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:18 |                             
 20230725101959 | Tue Jul 25 2023 10:19:59 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:22 |                             
 20230725101152 | Tue Jul 25 2023 10:11:52 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:18 |                             
 20230725101115 | Tue Jul 25 2023 10:11:15 | Success | demo     | full          |                  | gpbackup_s3_plugin | 00:00:20 |                             
 20230724090000 | Mon Jul 24 2023 09:00:00 | Success | demo     | metadata-only |                  | gpbackup_s3_plugin | 00:05:17 |                             
 20230723082000 | Sun Jul 23 2023 08:20:00 | Success | demo     | data-only     |                  | gpbackup_s3_plugin | 00:35:17 |                             
 20230722100000 | Sat Jul 22 2023 10:00:00 | Success | demo     | full          |                  | gpbackup_s3_plugin | 00:25:17 |                             
 20230721090000 | Fri Jul 21 2023 09:00:00 | Success | demo     | metadata-only |                  | gpbackup_s3_plugin | 00:04:17 |                             
 20230625110310 | Sun Jun 25 2023 11:03:10 | Success | demo     | incremental   | include-table    | gpbackup_s3_plugin | 00:40:18 | Plugin Backup Delete Failed 
 20230624101152 | Sat Jun 24 2023 10:11:52 | Success | demo     | incremental   | include-table    | gpbackup_s3_plugin | 00:30:00 |                             
 20230623101115 | Fri Jun 23 2023 10:11:15 | Success | demo     | full          | include-table    | gpbackup_s3_plugin | 01:01:00 |                             
 20230524101152 | Wed May 24 2023 10:11:52 | Success | demo     | incremental   | include-schema   | gpbackup_s3_plugin | 00:30:00 |                             
 20230523101115 | Tue May 23 2023 10:11:15 | Success | demo     | full          | include-schema   | gpbackup_s3_plugin | 01:01:00 |                             
 ```

Display info for active full backups from `gpbackup_history.db`:
```bash
./gpbackman backup-info \
  --type full

 TIMESTAMP      | DATE                     | STATUS  | DATABASE | TYPE | OBJECT FILTERING | PLUGIN             | DURATION | DATE DELETED 
----------------+--------------------------+---------+----------+------+------------------+--------------------+----------+--------------
 20230809232817 | Wed Aug 09 2023 23:28:17 | Success | demo     | full |                  |                    | 04:00:03 |              
 20230725101115 | Tue Jul 25 2023 10:11:15 | Success | demo     | full |                  | gpbackup_s3_plugin | 00:00:20 |              
 20230722100000 | Sat Jul 22 2023 10:00:00 | Success | demo     | full |                  | gpbackup_s3_plugin | 00:25:17 |              
 20230623101115 | Fri Jun 23 2023 10:11:15 | Success | demo     | full | include-table    | gpbackup_s3_plugin | 01:01:00 |              
 20230523101115 | Tue May 23 2023 10:11:15 | Success | demo     | full | include-schema   | gpbackup_s3_plugin | 01:01:00 |              
```

Find all backups, including deleted ones, containing the `test1` schema.
```bash
./gpbackman backup-info \
  --deleted \
  --schema test1

 TIMESTAMP      | DATE                     | STATUS  | DATABASE | TYPE        | OBJECT FILTERING | PLUGIN             | DURATION | DATE DELETED             
----------------+--------------------------+---------+----------+-------------+------------------+--------------------+----------+--------------------------
 20230525101152 | Thu May 25 2023 10:11:52 | Success | demo     | incremental | include-schema   | gpbackup_s3_plugin | 00:30:00 | Sun Jun 25 2023 10:11:52 
 20230524101152 | Wed May 24 2023 10:11:52 | Success | demo     | incremental | include-schema   | gpbackup_s3_plugin | 00:30:00 |                          
 20230523101115 | Tue May 23 2023 10:11:15 | Success | demo     | full        | include-schema   | gpbackup_s3_plugin | 01:01:00 |                          
 ```

Display info for all backups, including deleted and failed ones, from `gpbackup_history.db`:
```bash
./gpbackman backup-info \
  --deleted \
  --failed \
  --history-db /data/master/gpseg-1/gpbackup_history.db

 TIMESTAMP      | DATE                     | STATUS  | DATABASE | TYPE          | OBJECT FILTERING | PLUGIN             | DURATION | DATE DELETED                
----------------+--------------------------+---------+----------+---------------+------------------+--------------------+----------+-----------------------------
 20230809232817 | Wed Aug 09 2023 23:28:17 | Success | demo     | full          |                  |                    | 04:00:03 |                             
 20230806230400 | Sun Aug 06 2023 23:04:00 | Failure | demo     | full          |                  | gpbackup_s3_plugin | 00:00:38 |                             
 20230725110310 | Tue Jul 25 2023 11:03:10 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:18 | Wed Jul 26 2023 11:03:28    
 20230725110051 | Tue Jul 25 2023 11:00:51 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:20 |                             
 20230725102950 | Tue Jul 25 2023 10:29:50 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:19 |                             
 20230725102831 | Tue Jul 25 2023 10:28:31 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:18 |                             
 20230725101959 | Tue Jul 25 2023 10:19:59 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:22 |                             
 20230725101152 | Tue Jul 25 2023 10:11:52 | Success | demo     | incremental   |                  | gpbackup_s3_plugin | 00:00:18 |                             
 20230725101115 | Tue Jul 25 2023 10:11:15 | Success | demo     | full          |                  | gpbackup_s3_plugin | 00:00:20 |                             
 20230724090000 | Mon Jul 24 2023 09:00:00 | Success | demo     | metadata-only |                  | gpbackup_s3_plugin | 00:05:17 |                             
 20230723082000 | Sun Jul 23 2023 08:20:00 | Success | demo     | data-only     |                  | gpbackup_s3_plugin | 00:35:17 |                             
 20230722100000 | Sat Jul 22 2023 10:00:00 | Success | demo     | full          |                  | gpbackup_s3_plugin | 00:25:17 |                             
 20230721090000 | Fri Jul 21 2023 09:00:00 | Success | demo     | metadata-only |                  | gpbackup_s3_plugin | 00:04:17 |                             
 20230706230400 | Thu Jul 06 2023 23:04:00 | Failure | demo     | full          |                  | gpbackup_s3_plugin | 00:00:38 |                             
 20230625110310 | Sun Jun 25 2023 11:03:10 | Success | demo     | incremental   | include-table    | gpbackup_s3_plugin | 00:40:18 | Plugin Backup Delete Failed 
 20230624101152 | Sat Jun 24 2023 10:11:52 | Success | demo     | incremental   | include-table    | gpbackup_s3_plugin | 00:30:00 |                             
 20230623101115 | Fri Jun 23 2023 10:11:15 | Success | demo     | full          | include-table    | gpbackup_s3_plugin | 01:01:00 |                             
 20230606230400 | Tue Jun 06 2023 23:04:00 | Failure | demo     | full          |                  | gpbackup_s3_plugin | 00:00:38 |                             
 20230525101152 | Thu May 25 2023 10:11:52 | Success | demo     | incremental   | include-schema   | gpbackup_s3_plugin | 00:30:00 | Sun Jun 25 2023 10:11:52    
 20230524101152 | Wed May 24 2023 10:11:52 | Success | demo     | incremental   | include-schema   | gpbackup_s3_plugin | 00:30:00 |                             
 20230523101115 | Tue May 23 2023 10:11:15 | Success | demo     | full          | include-schema   | gpbackup_s3_plugin | 01:01:00 |                             
 ```

Display full backup with object filtering details:
```bash
./gpbackman backup-info \
  --type full \
  --detail

 TIMESTAMP      | DATE                     | STATUS  | DATABASE | TYPE | OBJECT FILTERING | PLUGIN             | DURATION | DATE DELETED | OBJECT FILTERING DETAILS 
----------------+--------------------------+---------+----------+------+------------------+--------------------+----------+--------------+--------------------------
 20250915221743 | Mon Sep 15 2025 22:17:43 | Success | demo     | full |                  |                    | 00:00:01 |              |                          
 20250915221643 | Mon Sep 15 2025 22:16:43 | Success | demo     | full | exclude-schema   | gpbackup_s3_plugin | 00:00:01 |              | sch1                     
 20250915221631 | Mon Sep 15 2025 22:16:31 | Success | demo     | full | include-table    | gpbackup_s3_plugin | 00:00:01 |              | sch2.tbl_c, sch2.tbl_d   
 20250915221616 | Mon Sep 15 2025 22:16:16 | Success | demo     | full |                  | gpbackup_s3_plugin | 00:00:05 |              |                          
 20250915221553 | Mon Sep 15 2025 22:15:53 | Success | demo     | full | exclude-table    |                    | 00:00:02 |              | sch1.tbl_b               
 20250915221542 | Mon Sep 15 2025 22:15:42 | Success | demo     | full | include-table    |                    | 00:00:01 |              | sch1.tbl_a               
 20250915221531 | Mon Sep 15 2025 22:15:31 | Success | demo     | full |                  |                    | 00:00:01 |              |                          

```

Display info for the backup chain for a specific backup. In this example, the backup with timestamp `20250913210921` is a full backup, and all its dependent incremental backups are displayed as well:
```bash
./gpbackman backup-info \
  --timestamp 20250913210921 \
  --detail

 TIMESTAMP      | DATE                     | STATUS  | DATABASE | TYPE        | OBJECT FILTERING | PLUGIN             | DURATION | DATE DELETED             | OBJECT FILTERING DETAILS 
----------------+--------------------------+---------+----------+-------------+------------------+--------------------+----------+--------------------------+--------------------------
 20250915201446 | Mon Sep 15 2025 20:14:46 | Success | demo     | incremental | include-table    | gpbackup_s3_plugin | 00:00:02 |                          | sch2.tbl_c               
 20250915201439 | Mon Sep 15 2025 20:14:39 | Success | demo     | incremental | include-table    | gpbackup_s3_plugin | 00:00:01 |                          | sch2.tbl_c               
 20250915201307 | Mon Sep 15 2025 20:13:07 | Success | demo     | incremental | include-table    | gpbackup_s3_plugin | 00:00:02 | Mon Sep 15 2025 20:17:56 | sch2.tbl_c               
 20250915200929 | Mon Sep 15 2025 20:09:29 | Success | demo     | incremental | include-table    | gpbackup_s3_plugin | 00:00:01 |                          | sch2.tbl_c               
 20250913210957 | Sat Sep 13 2025 21:09:57 | Success | demo     | incremental | include-table    | gpbackup_s3_plugin | 00:00:01 |                          | sch2.tbl_c               
 20250913210921 | Sat Sep 13 2025 21:09:21 | Success | demo     | full        | include-table    | gpbackup_s3_plugin | 00:00:02 |                          | sch2.tbl_c               
```

When using the option `--detail`, the column `OBJECT FILTERING DETAILS` may contain a large output. For pretty display, you can use `less -XS`:
```bash
./gpbackman backup-info --detail | less -XS
```

# Clean deleted backups from the history database (`history-clean`)

Available options for `history-clean` command and their description:

```bash
./gpbackman history-clean -h
Clean deleted backups from the history database.
Only the database is being cleaned up.

Information is deleted only about deleted backups from gpbackup_history.db. Each backup must be deleted first.

To delete information about backups older than the given timestamp, use the --before-timestamp option. 
To delete information about backups older than the given number of days, use the --older-than-day option. 
Only --older-than-days or --before-timestamp option must be specified, not both.

The gpbackup_history.db file location can be set using the --history-db option.
Can be specified only once. The full path to the file is required.
If the --history-db option is not specified, the history database will be searched in the current directory.

Usage:
  gpbackman history-clean [flags]

Flags:
      --before-timestamp string   delete information about backups older than the given timestamp
  -h, --help                      help for history-clean
      --older-than-days uint      delete information about backups older than the given number of days

Global Flags:
      --history-db string          full path to the gpbackup_history.db file
      --log-file string            full path to log file directory, if not specified, the log file will be created in the $HOME/gpAdminLogs directory
      --log-level-console string   level for console logging (error, info, debug, verbose) (default "info")
      --log-level-file string      level for file logging (error, info, debug, verbose) (default "info")
```

## Examples
### Delete information about deleted backups from history database older than n days
Delete information about deleted backups from history database older than 7 days:
```bash
./gpbackman history-clean \
  --older-than-days 7 \
```

### Delete information about deleted backups from history database older than timestamp
Delete information about deleted backups from history database older than timestamp `20240101100000`:
```bash
./gpbackman history-clean \
  --before-timestamp 20240101100000 \
```

# Display the report for a specific backup (`report-info`)

Available options for `report-info` command and their description:

```bash
./gpbackman.go report-info -h
Display the report for a specific backup.

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
If the --history-db option is not specified, the history database will be searched in the current directory.

Usage:
  gpbackman report-info [flags]

Flags:
      --backup-dir string                the full path to backup directory
  -h, --help                             help for report-info
      --plugin-config string             the full path to plugin config file
      --plugin-report-file-path string   the full path to plugin report file
      --timestamp string                 the backup timestamp for report displaying

Global Flags:
      --history-db string          full path to the gpbackup_history.db file
      --log-file string            full path to log file directory, if not specified, the log file will be created in the $HOME/gpAdminLogs directory
      --log-level-console string   level for console logging (error, info, debug, verbose) (default "info")
      --log-level-file string      level for file logging (error, info, debug, verbose) (default "info")
```

## Examples
### Display the backup report from local storage

With specifying backup directory path:
```bash
./gpbackman report-info \
  --timestamp 20230809232817 \
  --backup-dir /some/path
```

With specifying backup directory path:
```bash
./gpbackman report-info \
  --timestamp 20230809232817 \
```

### Display the backup report using storage plugin

For `gpbackup_s3_plugin`:
```bash
./gpbackman report-info \
  --timestamp 20230725101959 \
  --plugin-config /tmp/gpbackup_plugin_config.yaml
```

For other plugins:
```bash
./gpbackman report-infodoc \
  --timestamp 20230725101959 \
  --plugin-config /tmp/gpbackup_plugin_config.yaml \
  --plugin-report-file-path /some/path/to/report
```
