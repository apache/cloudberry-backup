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

The project requires the Go Programming language version 1.11 or higher.
Follow the directions [here](https://golang.org/doc/) for installation, usage
and configuration instructions. Make sure to set the [Go PATH environment
variable](https://go.dev/doc/install) before starting the following steps.

## Download & Build

1. Downloading the latest version:

```bash
go install github.com/apache/cloudberry-backup@latest
```

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

* To run all tests except end-to-end (linters, unit, and integration), use `make test`.
* To run only unit tests, use `make unit`.
* To run only integration tests (requires a running Cloudberry instance), use `make integration`.
* To run end to end tests (requires a running Cloudberry instance), use `make end_to_end`.

We provide the following targets to help developers ensure their code fits
Go standard formatting guidelines:

* To run a linting tool that checks for basic coding errors, use: `make lint`.
This target runs [gometalinter](https://github.com/alecthomas/gometalinter).
Note: The lint target will fail if code is not formatted properly.

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