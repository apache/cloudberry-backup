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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("LastBackupMetrics", func() {
	Describe("getBackupLastMetrics", func() {
		It("sets last backup metrics correctly", func() {
			resetLastBackupMetrics()
			getBackupLastMetrics(
				lastBackupMap{
					"test": backupMap{
						"full":          returnTimeTime("20230118150000"),
						"incremental":   returnTimeTime("20230118160000"),
						"metadata-only": returnTimeTime("20230118170000"),
						"data-only":     returnTimeTime("20230118180000"),
					},
				},
				templateUnixTime(),
				setUpMetricValue,
				getLogger(),
			)
			reg := prometheus.NewRegistry()
			reg.MustRegister(gpbckpBackupSinceLastCompletionSecondsMetric)
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
			templateMetrics := `# HELP gpbackup_backup_since_last_completion_seconds Seconds since the last completed backup.
# TYPE gpbackup_backup_since_last_completion_seconds gauge
gpbackup_backup_since_last_completion_seconds{backup_type="data-only",database_name="test"} 7200
gpbackup_backup_since_last_completion_seconds{backup_type="full",database_name="test"} 18000
gpbackup_backup_since_last_completion_seconds{backup_type="incremental",database_name="test"} 14400
gpbackup_backup_since_last_completion_seconds{backup_type="metadata-only",database_name="test"} 10800
`
			Expect(out.String()).To(Equal(templateMetrics))
		})
	})

	Describe("getBackupLastMetrics errors and debugs", func() {
		It("counts errors and debugs correctly", func() {
			resetLastBackupMetrics()
			out := &bytes.Buffer{}
			lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelDebug}))
			getBackupLastMetrics(
				lastBackupMap{
					"test": backupMap{
						"full": returnTimeTime("20230118150000"),
					},
				},
				templateUnixTime(),
				fakeSetUpMetricValue,
				lc,
			)
			errorsOutputCount := strings.Count(out.String(), "level=ERROR")
			debugsOutputCount := strings.Count(out.String(), "level=DEBUG")
			Expect(errorsOutputCount).To(Equal(1))
			Expect(debugsOutputCount).To(Equal(1))
		})
	})
})
