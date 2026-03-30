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

package cmd

import (
	"database/sql"

	"github.com/apache/cloudberry-backup/utils"
)

type backupDeleteInterface interface {
	backupDeleteDB(backupName string, hDB *sql.DB, ignoreErrors bool) error
}

type backupPluginDeleter struct {
	pluginConfigPath string
	pluginConfig     *utils.PluginConfig
}

func (bpd *backupPluginDeleter) backupDeleteDB(backupName string, hDB *sql.DB, ignoreErrors bool) error {
	return backupDeleteDBPluginFunc(backupName, bpd.pluginConfigPath, bpd.pluginConfig, hDB, ignoreErrors)
}

type backupLocalDeleter struct {
	backupDir            string
	maxParallelProcesses int
}

func (bld *backupLocalDeleter) backupDeleteDB(backupName string, hDB *sql.DB, ignoreErrors bool) error {
	return backupDeleteDBLocalFunc(backupName, bld.backupDir, bld.maxParallelProcesses, hDB, ignoreErrors)
}
