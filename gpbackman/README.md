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

# gpBackMan

**gpBackMan** is designed to manage backups created by gpbackup.

The utility works with `gpbackup_history.db` SQLite history database format. 

**gpBackMan** provides the following features:
* display information about backups;
* display the backup report for existing backups;
* delete existing backups from local storage or using storage plugins;
* delete all existing backups from local storage or using storage plugins older than the specified time condition;
* clean deleted backups from the history database;
* manually synchronize the cluster `gpbackup_history.db` to the standby coordinator;
* automatically synchronize the cluster `gpbackup_history.db` after successful backup deletion and history cleanup.

## Commands
### Introduction

Available commands and global options:

```bash
./gpbackman --help
gpBackMan - utility for managing backups created by gpbackup

Usage:
  gpbackman [command]

Available Commands:
  backup-clean  Delete all existing backups older than the specified time condition
  backup-delete Delete a specific existing backup
  backup-info   Display information about backups
  completion    Generate the autocompletion script for the specified shell
  help          Help about any command
  history-clean Clean deleted backups from the history database
  history-sync  Sync the history database to the standby coordinator
  report-info   Display the report for a specific backup

Flags:
      --auto-load-history-db       resolve gpbackup_history.db from $COORDINATOR_DATA_DIRECTORY when --history-db is unset
  -h, --help                       help for gpbackman
      --history-db string          full path to the gpbackup_history.db file
      --log-file string            full path to log file directory, if not specified, the log file will be created in the $HOME/gpAdminLogs directory
      --log-level-console string   level for console logging (error, info, debug, verbose) (default "info")
      --log-level-file string      level for file logging (error, info, debug, verbose) (default "info")
  -v, --version                    version for gpbackman

Use "gpbackman [command] --help" for more information about a command.
```

### Standby history DB sync

Run `history-sync` to explicitly synchronize the cluster `gpbackup_history.db` to an up standby coordinator. The source must resolve to `<primary coordinator data directory>/gpbackup_history.db`; a custom database or the default working-directory database is not eligible. Explicit sync treats every non-sync outcome as an error and exits non-zero.

For the usual cluster setup, resolve the source from the coordinator data directory:

```bash
./gpbackman history-sync --auto-load-history-db
```

After a successful `backup-delete`, `backup-clean`, or `history-clean`, gpBackMan also attempts the same synchronization automatically. Automatic sync is best-effort: ineligible source paths and no standby are debug-only skips, while sync failures are warnings and do not change the successful primary command result. Pass `--no-history-sync-standby` to those mutation commands to disable automatic sync.

Configure the sync timeout with `--history-sync-standby-timeout SECONDS` on
`history-sync`, `backup-delete`, `backup-clean`, and `history-clean`. The
default is 300 seconds; the supported range is 1 to 86400 seconds. The timeout
is one shared budget for `rsync` and remote install. It starts after snapshot
validation. Standby discovery and SQLite snapshot creation and validation
(`VACUUM INTO` and `PRAGMA quick_check`) are outside this budget. If a
transport step fails, remote cleanup of the temporary file uses its own fixed
120-second timeout, independent of `--history-sync-standby-timeout`.

`rsync` 3.0.0 or later must be installed on both the host running gpBackMan
and the standby coordinator. The current OS user must have non-interactive SSH
access to the standby host.

Only `gpbackup_history.db` is synchronized. Report files, backup data, and other backup artifacts are not synchronized.

### Detail info about commands

Description of each command:
* [Delete all existing backups older than the specified time condition (`backup-clean`)](./COMMANDS.md#delete-all-existing-backups-older-than-the-specified-time-condition-backup-clean)
* [Delete a specific existing backup (`backup-delete`)](./COMMANDS.md#delete-a-specific-existing-backup-backup-delete)
* [Display information about backups (`backup-info`)](./COMMANDS.md#display-information-about-backups-backup-info)
* [Clean deleted backups from the history database (`history-clean`)](./COMMANDS.md#clean-deleted-backups-from-the-history-database-history-clean)
* [Sync the history database to the standby coordinator (`history-sync`)](./COMMANDS.md#sync-the-history-database-to-the-standby-coordinator-history-sync)
* [Display the report for a specific backup (`report-info`)](./COMMANDS.md#display-the-report-for-a-specific-backup-report-info)

## About

gpBackMan is part of the Apache Cloudberry Backup (Incubating) toolset. It is based on the original [gpbackman](https://github.com/woblerr/gpbackman) project.
