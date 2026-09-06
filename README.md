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
# Backup Utility for Apache Cloudberry (Incubating)

[![Slack](https://img.shields.io/badge/Join_Slack-6a32c9)](https://communityinviter.com/apps/cloudberrydb/welcome)
[![Twitter Follow](https://img.shields.io/twitter/follow/cloudberrydb)](https://twitter.com/cloudberrydb)
[![Website](https://img.shields.io/badge/Visit%20Website-eebc46)](https://cloudberry.apache.org)

---

`gpbackup` and `gprestore` are Go utilities for performing Greenplum database
backups, which are originally developed by the Greenplum Database team. This
repo is a fork of gpbackup, dedicated to supporting Cloudberry.

## Pre-Requisites

The project requires the Go Programming language version 1.25 or higher.
Follow the directions [here](https://golang.org/doc/) for installation, usage
and configuration instructions. Make sure to set the [Go PATH environment
variable](https://go.dev/doc/install) before starting the following steps.

```
export GOPATH=$HOME/go
export PATH=$PATH:/usr/local/go/bin:$GOPATH/bin
```

## Download & Build

1. Downloading:

```bash
# Download the stable release version
go install github.com/apache/cloudberry-backup@2.1.0-incubating

# Or download the latest development version from main branch
go install github.com/apache/cloudberry-backup@main
```

**Note:** Please use the specific version `@2.1.0-incubating` or `@main` instead of `@latest`. The `@latest` tag will install an older version due to Go modules version resolution rules.

This will place the code in `$GOPATH/pkg/mod/github.com/apache/cloudberry-backup`.

2. Building and installing binaries

Make the `gpbackup` directory your current working directory and run:

```bash
make depend
make build
```

The `build` target will put the `gpbackup` and `gprestore` binaries in
`$HOME/go/bin`. This will also attempt to copy `gpbackup_helper` to the
Cloudberry segments (retrieving hostnames from `gp_segment_configuration`).
Pay attention to the output as it will indicate whether this operation was
successful.

`make build_linux` is for cross compiling on macOS, and the target is Linux.

`make install` will scp the `gpbackup_helper` binary (used with -single-data-file flag) to all hosts

## Running the utilities

The basic command for gpbackup is
```bash
gpbackup --dbname <your_db_name>
```

The basic command for gprestore is
```bash
gprestore --timestamp <YYYYMMDDHHMMSS>
```

Run `--help` with either command for a complete list of options.

### Standby history database synchronization

After a successful backup, `gpbackup` automatically copies a consistent
snapshot of the coordinator's `gpbackup_history.db` to an up standby
coordinator. Synchronization starts only after the final `Success` history row
has been written and the local SQLite connection has been closed.

This synchronization is best effort. If no up standby exists, synchronization
is skipped. If discovery, snapshot creation, transfer, or installation fails,
`gpbackup` logs a warning but keeps the successful backup exit status. A
failed or terminated backup is not synchronized. Synchronization also does not
run when `--no-history` is used or when the final history update fails. Use
`--no-history-sync-standby` to keep writing local history while disabling
standby synchronization for one backup:

```bash
gpbackup --dbname <your_db_name> --no-history-sync-standby
```

Configure the sync timeout with `--history-sync-standby-timeout SECONDS`. The
default is 300 seconds; the supported range is 1 to 86400 seconds. The timeout
is one shared budget for `rsync` and remote install. It starts after snapshot
validation. Standby discovery and SQLite snapshot creation and validation
(`VACUUM INTO` and `PRAGMA quick_check`) are outside this budget. If a
transport step fails, remote cleanup of the temporary file uses its own fixed
120-second timeout, independent of `--history-sync-standby-timeout`.

The synchronization process:

1. Takes a non-waiting lock next to the canonical source database.
2. Creates a consistent SQLite snapshot with `VACUUM INTO` and accepts it only
   when `PRAGMA quick_check` returns `ok`.
3. Transfers the snapshot with `rsync -p -s` to a unique temporary file in the
   standby coordinator data directory.
4. Preserves the existing standby file's owner, group, and mode when it
   exists, then atomically renames the temporary file to
   `gpbackup_history.db`.

`rsync` 3.0.0 or later must be installed on both the host running `gpbackup`
and the standby coordinator. The `gpbackup` host must also have `ssh`, and the
current OS user must have non-interactive SSH access to the standby host. That
user must be able to create files in the standby coordinator data directory
and preserve the destination file's ownership and permissions. The cluster
must expose an up standby in `gp_segment_configuration`.

The atomic rename prevents readers from observing a partially copied database,
but it is not a failover coordination mechanism. A coordinator role change
during synchronization can race with discovery and installation. Processes
that already have the old standby database open continue reading that old
inode until they close and reopen it.

For automatic synchronization after history maintenance and for the strict
manual command, see [gpBackMan history synchronization](./gpbackman/README.md#standby-history-db-sync).

## Additional tools

This repository also includes the following tools:

* [gpbackup_s3_plugin](./plugins/s3plugin/README.md) — S3 storage plugin for gpbackup and gprestore.
* [gpBackMan](./gpbackman/README.md) — utility for managing backups created by gpbackup.
* [gpbackup_exporter](./exporter/README.md) — Prometheus exporter for collecting metrics from gpbackup history database.

## Validation and code quality

### Test setup

Required for Cloudberry 1.0+, several tests require the
`dummy_seclabel` Cloudberry contrib module. This module exists only to
support regression testing of the SECURITY LABEL statement. It is not
intended to be used in production. Use the following commands to
install the module.

```bash
pushd $(find ~/workspace/cloudberry -name dummy_seclabel)
    make install
    gpconfig -c shared_preload_libraries -v dummy_seclabel
    gpstop -ra
    gpconfig -s shared_preload_libraries | grep dummy_seclabel
popd
```

### Test execution

**NOTE**: The integration and end_to_end tests require a running Cloudberry instance.

* To build and run unit and integration tests, use `make test`.
* To run only unit tests, use `make unit`.
* To run only integration tests (requires a running Cloudberry instance), use `make integration`.
* To run end to end tests (requires a running Cloudberry instance), use `make end_to_end`.

We provide the following targets to help developers ensure their code fits
Go standard formatting guidelines:

* To run a linting tool that checks for basic coding errors, use: `make lint`.
This target runs [golangci-lint](https://golangci-lint.run/) installed in
`$(GOPATH)/bin`. CI installs its pinned version through the official GitHub
Action.

* To automatically format your code and add/remove imports, use `make format`.
This target runs
[goimports](https://godoc.org/golang.org/x/tools/cmd/goimports) and
[gofmt](https://golang.org/cmd/gofmt/). We will only accept code that has been
formatted using this target or an equivalent `gofmt` call.

### Cleaning up

To remove the compiled binaries and other generated files, run `make clean`.

## Code Formatting

We use `goimports` to format go code. See
https://godoc.org/golang.org/x/tools/cmd/goimports The following command
formats the gpbackup codebase excluding the vendor directory and also lists
the files updated.

```bash
goimports -w -l $(find . -type f -name '*.go' -not -path "./vendor/*")
```

## Troubleshooting

1. Dummy Security Label module is not installed or configured

If you see errors in many integration tests (below), review the Validation and
code quality [Test setup](##Test setup) section above:

```
SECURITY LABEL FOR dummy ON TYPE public.testtype IS 'unclassified';
      Expected
          <pgx.PgError>: {
              Severity: "ERROR",
              Code: "22023",
              Message: "security label provider \"dummy\" is not loaded",
```

2. Tablespace already exists

If you see errors indicating the `test_tablespace` tablespace already exists
(below), execute `psql postgres -c 'DROP TABLESPACE test_tablespace'` to
cleanup the environment and rerun the tests.

```
    CREATE TABLESPACE test_tablespace LOCATION '/tmp/test_dir'
    Expected
        <pgx.PgError>: {
            Severity: "ERROR",
            Code: "42710",
            Message: "tablespace \"test_tablespace\" already exists",
```

## How to Contribute

See [CONTRIBUTING.md file](./CONTRIBUTING.md).

## License

Licensed under Apache License Version 2.0. For more details, please refer to
the [LICENSE](./LICENSE).

## Acknowledgment

Thanks to all the Greenplum Backup contributors, more details in its [GitHub
page](https://github.com/greenplum-db/gpbackup-archive).
