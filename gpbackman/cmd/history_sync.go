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
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/spf13/cobra"

	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
)

var historySyncCmd = &cobra.Command{
	Use:   "history-sync",
	Short: "Sync the history database to the standby coordinator",
	Long: `Sync the gpbackup_history.db file to the standby coordinator.

The command uses the cluster history database from --history-db, or from
$COORDINATOR_DATA_DIRECTORY when --auto-load-history-db is set. It succeeds
only after the standby file is replaced atomically with a verified snapshot.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doRootFlagValidation(cmd.Flags(), checkFileExistsConst)
		doHistorySync()
	},
}

func init() {
	rootCmd.AddCommand(historySyncCmd)
	historySyncCmd.Flags().IntVar(
		&historyStandbySyncTimeoutSeconds,
		historySyncStandbyTimeoutFlagName,
		historySyncStandbyTimeoutDefault,
		"shared rsync and remote install timeout in seconds; must be an integer between 1 and 86400",
	)
}

func doHistorySync() {
	logHeadersDebug()
	if err := syncHistoryStandbyStrict(); err != nil {
		gplog.Error("%s", textmsg.ErrorTextUnableSyncHistoryDBToStandby(err))
		execOSExit(exitErrorCode)
	}
}
