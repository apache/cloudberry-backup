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

# gpbackup_exporter

**gpbackup_exporter** is a Prometheus exporter for collecting metrics from gpbackup history database (`gpbackup_history.db`).

## Metrics
### Backup metrics
| Metric | Description |  Labels | Additional Info |
| ----------- | ------------------ | ------------- | --------------- |
| `gpbackup_backup_status` | backup status | backup_type, database_name, object_filtering, plugin, timestamp | Values description:<br> `0` - success,<br> `1` - failure.|
| `gpbackup_backup_deletion_status` | backup deletion status | backup_type, database_name, date_deleted, object_filtering, plugin, timestamp | Values description:<br> `0` - backup still exists,<br> `1` - backup was successfully deleted,<br> `2` - the deletion is in progress,<br> `3` - last delete attempt failed to delete backup from plugin storage,<br> `4` - last delete attempt failed to delete backup from local storage.|
| `gpbackup_backup_info` | backup info | backup_dir, backup_ver, backup_type, compression_type, database_name, database_ver, object_filtering, plugin, plugin_ver, timestamp, with_statistic | Values description:<br> `1` - info about backup is exist.|
| `gpbackup_backup_duration_seconds` | backup duration in seconds| backup_type, database_name, end_time, object_filtering, plugin, timestamp ||

### Last backup metrics
| Metric | Description |  Labels | Additional Info |
| ----------- | ------------------ | ------------- | --------------- |
| `gpbackup_backup_since_last_completion_seconds`| seconds since the last completed backup | backup_type, database_name ||

### Exporter metrics

| Metric | Description |  Labels | Additional Info |
| ----------- | ------------------ | ------------- | --------------- |
| `gpbackup_exporter_build_info` | information about gpbackup exporter | branch, goarch, goos, goversion, revision, tags, version | |
| `gpbackup_exporter_status` | gpbackup exporter get data status | database_name | Values description:<br> `0` - errors occurred when fetching information from history database,<br> `1` - information successfully fetched from history database. |

## Getting Started
Available configuration flags:

```bash
./gpbackup_exporter --help
usage: gpbackup_exporter [<flags>]


Flags:
  -h, --[no-]help                Show context-sensitive help (also try --help-long and --help-man).
      --web.telemetry-path="/metrics"  
                                 Path under which to expose metrics.
      --web.listen-address=:19854 ...  
                                 Addresses on which to expose metrics and web interface. Repeatable for multiple addresses. Examples: `:9100` or `[::1]:9100` for http, `vsock://:9100` for vsock
      --web.config.file=""       Path to configuration file that can enable TLS or authentication. See: https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md
      --collect.interval=600     Collecting metrics interval in seconds.
      --collect.depth=0          Metrics depth collection in days. Metrics for backup older than this interval will not be collected. 0 - disable.
      --gpbackup.history-file=""  
                                 Path to gpbackup_history.db.
      --gpbackup.db-include="" ...  
                                 Specific db for collecting metrics. Can be specified several times.
      --gpbackup.db-exclude="" ...  
                                 Specific db to exclude from collecting metrics. Can be specified several times.
      --gpbackup.backup-type=""  Specific backup type for collecting metrics. One of: [full, incremental, data-only, metadata-only].
      --[no-]gpbackup.collect-deleted  
                                 Collecting metrics for deleted backups.
      --[no-]gpbackup.collect-failed  
                                 Collecting metrics for failed backups.
      --log.level=info           Only log messages with the given severity or above. One of: [debug, info, warn, error]
      --log.format=logfmt        Output format of log messages. One of: [logfmt, json]
      --[no-]version             Show application version.
```

### Additional description of flags.

It's necessary to specify the `gpbackup_history.db` file location via `--gpbackup.history-file` flag.

By default, metrics a collected only for active backups. The flag `--gpbackup.collect-deleted` allows to collect metrics for deleted backups. The flag `--gpbackup.collect-failed` allows to collect metrics for failed backups. 

Custom database for collecting metrics can be specified via `--gpbackup.db-include` flag. You can specify several databases.<br>
For example, `--gpbackup.db-include=demo1 --gpbackup.db-include=demo2`.<br>
For this case, metrics will be collected only for `demo1` and `demo2` databases.

Custom database to exclude from collecting metrics can be specified via `--gpbackup.db-exclude` flag. You can specify several databases.<br>
For example, `--gpbackup.db-exclude=demo1 --gpbackup.db-exclude=demo2`.<br>
For this case, metrics **will not be collected** for `demo1` and `demo2` databases.<br>
If the same database is specified for include and exclude flags, then metrics for this database will not be collected. 
The flag `--gpbackup.db-exclude` has a higher priority.<br>
For example, `--gpbackup.db-include=demo1 --gpbackup.db-exclude=demo1`.<br>
For this case, metrics **will not be collected** for `demo1` database.

Custom `backup type` for collecting metrics can be specified via `--gpbackup.backup-type` flag. Valid values: `full`, `incremental`, `data-only`, `metadata-only`.<br>
For example, `--gpbackup.backup-type=full`.<br>
For this case, metrics will be collected only for `full` backups.<br>

Custom metrics depth collection in days can be specified via `--collect.depth` flag. Since gpbackup doesn't have regular options for removing info about outdated backups from history file, it is possible to limit the depth of collection metrics.<br>
For example, `--collect.depth=14`.<br> 
For this case, metrics will be collected for backups not older then 14 days from current time.<br>
Value `0` - disable this functionality.

When `--log.level=debug` is specified - information of values and labels for metrics is printing to the log.

The flag `--web.config.file` allows to specify the path to the configuration for TLS and/or basic authentication.

## Running

```bash
./gpbackup_exporter \
    --gpbackup.history-file=/data/master/gpseg-1/gpbackup_history.db \
    --gpbackup.collect-deleted \
    --gpbackup.collect-failed
```

After starting, metrics are available at `http://localhost:19854/metrics`.

## About

gpbackup_exporter is part of the Apache Cloudberry Backup (Incubating) toolset. It is based on the original [gpbackup_exporter](https://github.com/woblerr/gpbackup_exporter) project.
