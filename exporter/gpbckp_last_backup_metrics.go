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

package exporter

import (
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var gpbckpBackupSinceLastCompletionSecondsMetric = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "gpbackup_backup_since_last_completion_seconds",
	Help: "Seconds since the last completed backup.",
},
	[]string{
		"backup_type",
		"database_name"})

// Set backup metrics:
//   - gpbackup_backup_since_last_completion_seconds
func getBackupLastMetrics(lastBackups lastBackupMap, currentUnixTime int64, setUpMetricValueFun setUpMetricValueFunType, logger *slog.Logger) {
	for db, bckps := range lastBackups {
		for bckpType, endTime := range bckps {
			// Seconds since the last completed backups.
			setUpMetric(
				gpbckpBackupSinceLastCompletionSecondsMetric,
				"gpbackup_backup_since_last_completion_seconds",
				time.Unix(currentUnixTime, 0).Sub(endTime).Seconds(),
				setUpMetricValueFun,
				logger,
				bckpType,
				db,
			)
		}
	}
}

func resetLastBackupMetrics() {
	gpbckpBackupSinceLastCompletionSecondsMetric.Reset()
}
