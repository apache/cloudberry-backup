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
* synchronize the cluster history database to the standby coordinator.

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

### Detail info about commands

Description of each command:
* [Delete all existing backups older than the specified time condition (`backup-clean`)](./COMMANDS.md#delete-all-existing-backups-older-than-the-specified-time-condition-backup-clean)
* [Delete a specific existing backup (`backup-delete`)](./COMMANDS.md#delete-a-specific-existing-backup-backup-delete)
* [Display information about backups (`backup-info`)](./COMMANDS.md#display-information-about-backups-backup-info)
* [Clean deleted backups from the history database (`history-clean`)](./COMMANDS.md#clean-deleted-backups-from-the-history-database-history-clean)
* [Sync the history database to the standby coordinator (`history-sync`)](./COMMANDS.md#sync-the-history-database-to-the-standby-coordinator-history-sync)
* [Display the report for a specific backup (`report-info`)](./COMMANDS.md#display-the-report-for-a-specific-backup-report-info)

## Standby history database synchronization

`backup-delete`, `backup-clean`, and `history-clean` automatically attempt to
synchronize `gpbackup_history.db` after the command has successfully finished
and closed the database. Successful no-op commands also attempt
synchronization. For `backup-delete --ignore-errors`, synchronization runs
when the command completes with its existing successful result, including when
individual deletion errors were recorded.

Automatic synchronization is best effort. A normal skip, such as no up
standby or an ineligible history database, does not fail the mutation command.
A discovery, snapshot, transfer, or installation error is logged as a warning
without changing an otherwise successful exit status. Use the command-local
`--no-history-sync-standby` flag to disable the attempt for one of these three
commands. The flag is not available on read-only commands or `history-sync`.

`history-sync` provides strict synchronization:

```bash
./gpbackman history-sync --auto-load-history-db
```

It exits successfully only after a verified snapshot has been installed
atomically on an up standby. An ineligible source, no up standby, a busy sync
lock, or any discovery, snapshot, transfer, or installation error causes a
nonzero exit.

The source must resolve to
`<primary-coordinator-data-directory>/gpbackup_history.db`. An explicit
`--history-db` path is accepted only when its canonical path is that cluster
database; a symlink to that file is accepted. A custom database is not
synchronized. With `--auto-load-history-db`, gpBackMan resolves the file from
`$COORDINATOR_DATA_DIRECTORY`. Without either option, gpBackMan uses the
working-directory database for normal command behavior, but standby
synchronization skips it; therefore a strict `history-sync` call fails.
`--history-db` takes precedence when both global options are present.

Synchronization requires an up standby coordinator, `ssh` and `rsync` on the
host running gpBackMan, and non-interactive SSH access for the current OS user.
The user must be able to read the primary history database, create its adjacent
`.sync.lock`, write temporary files in the standby coordinator data directory,
and preserve the existing standby file's owner, group, and mode.

gpBackMan creates and validates a consistent SQLite snapshot before transfer,
copies it to a unique standby temporary path, and atomically renames it into
place. Readers never see a partial copy. This does not coordinate a concurrent
coordinator failover: a role change can race with discovery and installation,
and a process that already has the old database open continues using that old
inode until it reopens the file.

## About

gpBackMan is part of the Apache Cloudberry Backup (Incubating) toolset. It is based on the original [gpbackman](https://github.com/woblerr/gpbackman) project.
