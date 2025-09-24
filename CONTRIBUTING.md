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

# Contributing

Everyone who participates in Cloudberry, either as a user or a contributor, is obliged to follow the [Code of Conduct](./CODE-OF-CONDUCT.md).

## Getting Started

To get started, follow these steps:
* Fork the `cloudberry-backup` repository on GitHub
* Run `go get github.com/apache/cloudberry-backup/...` and add your fork as a remote
* Run `make depend` to install required dependencies
* Follow the README to set up your environment and run the tests

## Creating a change

* Create your own feature branch (e.g. `git checkout -b new_branch`) and make changes on this branch.
* Try and follow similar coding styles as found throughout the codebase.
* Make commits as logical units for ease of reviewing.
* Rebase with main often to stay in sync with upstream.
* Add new tests to cover your code. We use [Ginkgo](http://onsi.github.io/ginkgo/) and [Gomega](https://onsi.github.io/gomega/) for testing.
* Ensure a well written commit message as explained [here](https://chris.beams.io/posts/git-commit/).
* Run `make format`, `make test`, and `make end_to_end` in your feature branch and ensure they are successful.
* Push your local branch to the fork (e.g. `git push <your_fork> backup_branch`)
* Create a Pull Request from your fork