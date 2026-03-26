# gpBackMan

**gpBackMan** is designed to manage backups created by gpbackup.

The utility works with `gpbackup_history.db` SQLite history database format. 

**gpBackMan** provides the following features:
* display information about backups;
* display the backup report for existing backups;
* delete existing backups from local storage or using storage plugins;
* delete all existing backups from local storage or using storage plugins older than the specified time condition;
* clean deleted backups from the history database;

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
  report-info   Display the report for a specific backup

Flags:
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
* [Display the report for a specific backup (`report-info`)](./COMMANDS.md#display-the-report-for-a-specific-backup-report-info)

## About

gpBackMan is part of the Apache Cloudberry Backup (Incubating) toolset. It is based on the original [gpbackman](https://github.com/woblerr/gpbackman) project.

