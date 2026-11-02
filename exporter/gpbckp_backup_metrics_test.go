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
	"bytes"
	"fmt"
	"log/slog"
	"strings"

	"github.com/apache/cloudberry-backup/history"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BackupMetrics", func() {
	// All metrics exist and all labels are corrected.
	// gpbackup version >= 1.23.0
	Describe("getBackupMetrics", func() {
		It("sets all backup metrics correctly", func() {
			resetBackupMetrics()
			getBackupMetrics(templateBackupConfig(), setUpMetricValue, getLogger())
			reg := prometheus.NewRegistry()
			reg.MustRegister(
				gpbckpBackupStatusMetric,
				gpbckpBackupDataDeletedStatusMetric,
				gpbckpBackupInfoMetric,
				gpbckpBackupDurationMetric,
			)
			metricFamily, err := reg.Gather()
			if err != nil {
				fmt.Println(err)
			}
			out := &bytes.Buffer{}
			for _, mf := range metricFamily {
				if _, err := expfmt.MetricFamilyToText(out, mf); err != nil {
					panic(err)
				}
			}
			templateMetrics := `# HELP gpbackup_backup_deletion_status Backup deletion status.
# TYPE gpbackup_backup_deletion_status gauge
gpbackup_backup_deletion_status{backup_type="full",database_name="test",date_deleted="none",object_filtering="none",plugin="none",timestamp="20230118152654"} 0
# HELP gpbackup_backup_duration_seconds Backup duration.
# TYPE gpbackup_backup_duration_seconds gauge
gpbackup_backup_duration_seconds{backup_type="full",database_name="test",end_time="20230118152656",object_filtering="none",plugin="none",timestamp="20230118152654"} 2
# HELP gpbackup_backup_info Backup info.
# TYPE gpbackup_backup_info gauge
gpbackup_backup_info{backup_dir="/data/backups",backup_type="full",backup_ver="1.30.5",compression_type="gzip",database_name="test",database_ver="6.23.0",object_filtering="none",plugin="none",plugin_ver="none",timestamp="20230118152654",with_statistic="false"} 1
# HELP gpbackup_backup_status Backup status.
# TYPE gpbackup_backup_status gauge
gpbackup_backup_status{backup_type="full",database_name="test",object_filtering="none",plugin="none",timestamp="20230118152654"} 0
`
			Expect(out.String()).To(Equal(templateMetrics))
		})
	})

	Describe("getBackupMetrics errors and debugs", func() {
		DescribeTable("counts errors and debugs correctly",
			func(backupData *history.BackupConfig, errorsCount, debugsCount int) {
				resetBackupMetrics()
				out := &bytes.Buffer{}
				lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))
				getBackupMetrics(backupData, fakeSetUpMetricValue, lc)
				errorsOutputCount := strings.Count(out.String(), "level=ERROR")
				debugsOutputCount := strings.Count(out.String(), "level=DEBUG")
				Expect(errorsOutputCount).To(Equal(errorsCount))
				Expect(debugsOutputCount).To(Equal(debugsCount))
			},
			Entry("GetBackupMetricsErrorGetDurationGood",
				templateBackupConfig(),
				4, 4,
			),
			Entry("GetBackupMetricsErrorGetDurationError",
				&history.BackupConfig{},
				5, 4,
			),
			Entry("GetBackupMetricsErrorGetBackupTypeAndObjectFilteringError",
				// Fake example for testing.
				&history.BackupConfig{
					DataOnly:              true,
					Incremental:           true,
					IncludeSchemaFiltered: true,
					ExcludeSchemaFiltered: true,
				},
				7, 4,
			),
		)
	})
})
