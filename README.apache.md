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

# Apache Cloudberry Backup (Incubating) License Audit Notes

This file documents licensing clarifications and exceptions as part of ASF release readiness for Apache Cloudberry Backup (Incubating).

## Historical Attribution Under Apache License 2.0

The following entities have contributed to the Greenplum Backup source code under the Apache License 2.0:

- Greenplum, Inc.
- EMC Corporation
- VMware, Inc.
- Pivotal Software

RAT matchers are used to classify their license headers accordingly.

## Binary Distribution Compliance

The source release bundles no third-party dependencies: there is no
`vendor/` directory, so `LICENSE` and `NOTICE` describe the source tree on
their own and are the files that apply to the source release.

The convenience binary packages are a different artifact. They are
statically linked Go binaries, so they physically contain the code of
every module in the build graph, plus the Go runtime and standard library
and — because CGO is enabled for SQLite support — the SQLite amalgamation.
None of that is present in the source release. Following the convention
used by Apache Spark, Apache Kafka and the Apache Cloudberry main
repository, those packages therefore ship:

| File in this repository | Installed in the package as | Contents |
| --- | --- | --- |
| `LICENSE-binary`  | `LICENSE`  | `LICENSE`, plus an inventory of every component bundled inside the binaries, grouped by license |
| `NOTICE-binary`   | `NOTICE`   | `NOTICE`, plus the NOTICE files of bundled Apache-licensed components, as required by section 4(d) of the Apache License 2.0 |
| `licenses-binary/` | `licenses/` | The verbatim license text of each bundled component, laid out by import path |

These three are generated from the build graph rather than maintained by
hand, so that adding, removing or bumping a dependency cannot silently
invalidate them:

```bash
scripts/generate-binary-license.sh
```

The script resolves the modules actually compiled into each shipped
binary, for each released platform, which means test-only dependencies
such as Ginkgo and Gomega are correctly excluded, while platform-gated
modules that only appear on Linux are correctly included.  Libraries that
stay outside the artifact and are resolved from the host at run time, such
as the system C library, are not bundled and so are not listed. `make package` copies
the generated files into the tarball, and the `binary-license-check` job
in the compliance workflow runs the script with `--check` to fail the
build when the committed files have drifted.

The license texts under `licenses-binary/` are reproduced unmodified from
upstream and carry their own copyright notices, so they are excluded from
the RAT scan and must not be given ASF headers.

## Compressed Files in Source

The following compressed files are included in the source tree. These files are archives of text files used for testing purposes and do not contain binary executables. They are not used during the build process.

- end_to_end/resources/1-segment-db-filter.tar.gz
- end_to_end/resources/1-segment-db-replicated.tar.gz
- end_to_end/resources/1-segment-db-single-data-file.tar.gz
- end_to_end/resources/1-segment-db.tar.gz
- end_to_end/resources/2-segment-db-1_24_0.tar.gz
- end_to_end/resources/2-segment-db-1_26_0.tar.gz
- end_to_end/resources/2-segment-db-filter.tar.gz
- end_to_end/resources/2-segment-db-incremental.tar.gz
- end_to_end/resources/2-segment-db-single-data-file-filter.tar.gz
- end_to_end/resources/2-segment-db-single-data-file.tar.gz
- end_to_end/resources/2-segment-db.tar.gz
- end_to_end/resources/3-segment-db-replicated.tar.gz
- end_to_end/resources/3-segment-db.tar.gz
- end_to_end/resources/5-segment-db.tar.gz
- end_to_end/resources/7-segment-db-filter.tar.gz
- end_to_end/resources/7-segment-db-single-data-file-filter.tar.gz
- end_to_end/resources/7-segment-db-single-data-file.tar.gz
- end_to_end/resources/7-segment-db.tar.gz
- end_to_end/resources/9-segment-db-incremental.tar.gz
- end_to_end/resources/9-segment-db-replicated.tar.gz
- end_to_end/resources/9-segment-db-single-data-file.tar.gz
- end_to_end/resources/9-segment-db.tar.gz
- end_to_end/resources/corrupt-db.tar.gz
- end_to_end/resources/corrupt-metadata-db.tar.gz
- end_to_end/resources/no-segment-count-db.tar.gz
