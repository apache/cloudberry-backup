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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var gpbckpExporterStatusMetric = promauto.NewGaugeVec(prometheus.GaugeOpts{
	Name: "gpbackup_exporter_status",
	Help: "gpbackup exporter get data status.",
},
	[]string{"database_name"})

// Set exporter metrics:
//   - gpbackup_exporter_status
func getExporterStatusMetrics(dbStatus dbStatusMap, setUpMetricValueFun setUpMetricValueFunType, logger *slog.Logger) {
	for dbName, status := range dbStatus {
		setUpMetric(
			gpbckpExporterStatusMetric,
			"gpbackup_exporter_status",
			convertBoolToFloat64(status),
			setUpMetricValueFun,
			logger,
			dbName,
		)
	}
}

func resetExporterMetrics() {
	gpbckpExporterStatusMetric.Reset()
}
