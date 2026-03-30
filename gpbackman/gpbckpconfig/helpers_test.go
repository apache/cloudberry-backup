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

package gpbckpconfig

import (
	"github.com/apache/cloudberry-backup/history"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("helpers tests", func() {
	Describe("GetBackupType", func() {
		It("returns correct backup type", func() {
			tests := []struct {
				name    string
				config  history.BackupConfig
				want    string
				wantErr bool
			}{
				{
					name:    "incremental backup",
					config:  history.BackupConfig{Incremental: true},
					want:    BackupTypeIncremental,
					wantErr: false,
				},
				{
					name:    "data-only backup",
					config:  history.BackupConfig{DataOnly: true},
					want:    BackupTypeDataOnly,
					wantErr: false,
				},
				{
					name:    "metadata-only backup",
					config:  history.BackupConfig{MetadataOnly: true},
					want:    BackupTypeMetadataOnly,
					wantErr: false,
				},
				{
					name:    "metadata-only when data-only also set",
					config:  history.BackupConfig{DataOnly: true, MetadataOnly: true},
					want:    BackupTypeMetadataOnly,
					wantErr: false,
				},
				{
					name:    "metadata-only when incremental also set",
					config:  history.BackupConfig{Incremental: true, MetadataOnly: true},
					want:    BackupTypeMetadataOnly,
					wantErr: false,
				},
				{
					name: "full backup",
					config: history.BackupConfig{
						Incremental:  false,
						DataOnly:     false,
						MetadataOnly: false,
					},
					want:    BackupTypeFull,
					wantErr: false,
				},
				{
					name:    "invalid backup case 1",
					config:  history.BackupConfig{Incremental: true, DataOnly: true},
					want:    "",
					wantErr: true,
				},
				{
					name:    "invalid backup case 2",
					config:  history.BackupConfig{Incremental: true, DataOnly: true, MetadataOnly: true},
					want:    "",
					wantErr: true,
				},
			}
			for _, tt := range tests {
				cfg := tt.config
				got, err := GetBackupType(&cfg)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
				}
				Expect(got).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("GetObjectFilteringInfo", func() {
		It("returns correct filtering info", func() {
			tests := []struct {
				name    string
				config  history.BackupConfig
				want    string
				wantErr bool
			}{
				{
					name:   "IncludeSchemaFiltered",
					config: history.BackupConfig{IncludeSchemaFiltered: true},
					want:   objectFilteringIncludeSchema,
				},
				{
					name:   "ExcludeSchemaFiltered",
					config: history.BackupConfig{ExcludeSchemaFiltered: true},
					want:   objectFilteringExcludeSchema,
				},
				{
					name:   "IncludeTableFiltered",
					config: history.BackupConfig{IncludeTableFiltered: true},
					want:   objectFilteringIncludeTable,
				},
				{
					name:   "ExcludeTableFiltered",
					config: history.BackupConfig{ExcludeTableFiltered: true},
					want:   objectFilteringExcludeTable,
				},
				{
					name:   "NoFiltering",
					config: history.BackupConfig{},
					want:   "",
				},
				{
					name:    "Invalid IncludeTable and ExcludeTable",
					config:  history.BackupConfig{IncludeTableFiltered: true, ExcludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid IncludeSchema and ExcludeSchema",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, ExcludeSchemaFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid IncludeSchema and IncludeTable",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, IncludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid IncludeSchema and ExcludeTable",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, ExcludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid ExcludeSchema and IncludeTable",
					config:  history.BackupConfig{ExcludeSchemaFiltered: true, IncludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid ExcludeSchema and ExcludeTable",
					config:  history.BackupConfig{ExcludeSchemaFiltered: true, ExcludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid IncludeSchema IncludeTable and ExcludeTable",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, IncludeTableFiltered: true, ExcludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid IncludeSchema ExcludeSchema and IncludeTable",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, ExcludeSchemaFiltered: true, IncludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid IncludeSchema ExcludeSchema and ExcludeTable",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, ExcludeSchemaFiltered: true, ExcludeTableFiltered: true},
					wantErr: true,
				},
				{
					name:    "Invalid all filters set",
					config:  history.BackupConfig{IncludeSchemaFiltered: true, ExcludeSchemaFiltered: true, IncludeTableFiltered: true, ExcludeTableFiltered: true},
					wantErr: true,
				},
			}
			for _, tt := range tests {
				cfg := tt.config
				got, err := GetObjectFilteringInfo(&cfg)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
					Expect(got).To(Equal(tt.want), tt.name)
				}
			}
		})
	})

	Describe("GetObjectFilteringDetails", func() {
		It("returns correct filtering details", func() {
			tests := []struct {
				name   string
				config history.BackupConfig
				want   string
			}{
				{
					name: "IncludeTable details",
					config: history.BackupConfig{
						IncludeTableFiltered: true,
						IncludeRelations:     []string{"public.t1", "s.t2"},
					},
					want: "public.t1, s.t2",
				},
				{
					name: "ExcludeTable details",
					config: history.BackupConfig{
						ExcludeTableFiltered: true,
						ExcludeRelations:     []string{"public.t3"},
					},
					want: "public.t3",
				},
				{
					name: "IncludeSchema details",
					config: history.BackupConfig{
						IncludeSchemaFiltered: true,
						IncludeSchemas:        []string{"public", "sales"},
					},
					want: "public, sales",
				},
				{
					name: "ExcludeSchema details",
					config: history.BackupConfig{
						ExcludeSchemaFiltered: true,
						ExcludeSchemas:        []string{"tmp"},
					},
					want: "tmp",
				},
				{
					name:   "No filtering",
					config: history.BackupConfig{},
					want:   "",
				},
			}
			for _, tt := range tests {
				cfg := tt.config
				got := GetObjectFilteringDetails(&cfg)
				Expect(got).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("GetBackupDate", func() {
		It("parses valid timestamp", func() {
			cfg := history.BackupConfig{Timestamp: "20220401102430"}
			got, err := GetBackupDate(&cfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal("Fri Apr 01 2022 10:24:30"))
		})

		It("returns error for invalid timestamp", func() {
			cfg := history.BackupConfig{Timestamp: "invalid"}
			_, err := GetBackupDate(&cfg)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetBackupDuration", func() {
		It("calculates duration correctly", func() {
			cfg := history.BackupConfig{
				Timestamp: "20220401102430",
				EndTime:   "20220401115502",
			}
			got, err := GetBackupDuration(&cfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(float64(5432)))
		})

		It("returns error for invalid start timestamp", func() {
			cfg := history.BackupConfig{
				Timestamp: "invalid",
				EndTime:   "20220401115502",
			}
			_, err := GetBackupDuration(&cfg)
			Expect(err).To(HaveOccurred())
		})

		It("returns error for invalid end timestamp", func() {
			cfg := history.BackupConfig{
				Timestamp: "20220401102430",
				EndTime:   "invalid",
			}
			_, err := GetBackupDuration(&cfg)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetBackupDateDeleted", func() {
		It("returns correct date deleted", func() {
			tests := []struct {
				name    string
				config  history.BackupConfig
				want    string
				wantErr bool
			}{
				{
					name:   "empty",
					config: history.BackupConfig{DateDeleted: ""},
					want:   "",
				},
				{
					name:   "in progress",
					config: history.BackupConfig{DateDeleted: DateDeletedInProgress},
					want:   DateDeletedInProgress,
				},
				{
					name:   "plugin backup delete failed",
					config: history.BackupConfig{DateDeleted: DateDeletedPluginFailed},
					want:   DateDeletedPluginFailed,
				},
				{
					name:   "local delete failed",
					config: history.BackupConfig{DateDeleted: DateDeletedLocalFailed},
					want:   DateDeletedLocalFailed,
				},
				{
					name:   "valid date",
					config: history.BackupConfig{DateDeleted: "20220401102430"},
					want:   "Fri Apr 01 2022 10:24:30",
				},
				{
					name:    "invalid date",
					config:  history.BackupConfig{DateDeleted: "InvalidDate"},
					want:    "InvalidDate",
					wantErr: true,
				},
			}
			for _, tt := range tests {
				cfg := tt.config
				got, err := GetBackupDateDeleted(&cfg)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
				}
				Expect(got).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("IsSuccess", func() {
		It("returns true for success status", func() {
			cfg := history.BackupConfig{Status: history.BackupStatusSucceed}
			got, err := IsSuccess(&cfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(BeTrue())
		})

		It("returns false for failure status", func() {
			cfg := history.BackupConfig{Status: history.BackupStatusFailed}
			got, err := IsSuccess(&cfg)
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(BeFalse())
		})

		It("returns error for unknown status", func() {
			cfg := history.BackupConfig{Status: "unknown"}
			_, err := IsSuccess(&cfg)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("IsLocal", func() {
		It("returns true when plugin is empty", func() {
			cfg := history.BackupConfig{Plugin: ""}
			Expect(IsLocal(&cfg)).To(BeTrue())
		})

		It("returns false when plugin is set", func() {
			cfg := history.BackupConfig{Plugin: "plugin"}
			Expect(IsLocal(&cfg)).To(BeFalse())
		})
	})

	Describe("IsInProgress", func() {
		It("returns correct result for various statuses", func() {
			tests := []struct {
				name   string
				status string
				want   bool
			}{
				{"in progress", history.BackupStatusInProgress, true},
				{"success", history.BackupStatusSucceed, false},
				{"failure", history.BackupStatusFailed, false},
				{"empty", "", false},
				{"unknown", "unknown", false},
			}
			for _, tt := range tests {
				cfg := history.BackupConfig{Status: tt.status}
				Expect(IsInProgress(&cfg)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("GetReportFilePathPlugin", func() {
		It("returns correct report path", func() {
			tests := []struct {
				name             string
				config           history.BackupConfig
				customReportPath string
				pluginOptions    map[string]string
				want             string
				wantErr          bool
			}{
				{
					name: "custom report path",
					config: history.BackupConfig{
						Timestamp: "20220401102430",
						Plugin:    BackupS3Plugin,
					},
					customReportPath: "/path/to/report",
					pluginOptions:    make(map[string]string),
					want:             "/path/to/report/gpbackup_20220401102430_report",
				},
				{
					name: "s3 plugin folder absent",
					config: history.BackupConfig{
						Timestamp: "20220401102430",
						Plugin:    BackupS3Plugin,
					},
					pluginOptions: map[string]string{"bucket": "bucket"},
					wantErr:       true,
				},
				{
					name: "s3 plugin folder empty",
					config: history.BackupConfig{
						Timestamp: "20220401102430",
						Plugin:    BackupS3Plugin,
					},
					pluginOptions: map[string]string{"folder": ""},
					wantErr:       true,
				},
				{
					name: "s3 plugin folder ok",
					config: history.BackupConfig{
						Timestamp: "20220401102430",
						Plugin:    BackupS3Plugin,
					},
					pluginOptions: map[string]string{"folder": "/path/to/report"},
					want:          "/path/to/report/backups/20220401/20220401102430/gpbackup_20220401102430_report",
				},
				{
					name: "unknown plugin without custom report path",
					config: history.BackupConfig{
						Timestamp: "20220401102430",
						Plugin:    "some_plugin",
					},
					pluginOptions: map[string]string{"folder": "/path/to/report"},
					wantErr:       true,
				},
			}
			for _, tt := range tests {
				cfg := tt.config
				got, err := GetReportFilePathPlugin(&cfg, tt.customReportPath, tt.pluginOptions)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
					Expect(got).To(Equal(tt.want), tt.name)
				}
			}
		})
	})

	Describe("CheckObjectFilteringExists", func() {
		It("returns correct result for various filtering scenarios", func() {
			tests := []struct {
				name          string
				tableFilter   string
				schemaFilter  string
				objectFilter  string
				excludeFilter bool
				want          bool
				config        history.BackupConfig
			}{
				{
					name: "no filters specified",
					want: true,
				},
				{
					name:         "table filter matches included table",
					tableFilter:  "test.table1",
					objectFilter: "include-table",
					want:         true,
					config: history.BackupConfig{
						IncludeRelations: []string{"test.table1", "test.table2"},
					},
				},
				{
					name:         "table filter does not match included table",
					tableFilter:  "test.table1",
					objectFilter: "include-table",
					want:         false,
					config: history.BackupConfig{
						IncludeRelations: []string{"test.table2", "test.table3"},
					},
				},
				{
					name:         "table filter with no object filter",
					tableFilter:  "test.table1",
					objectFilter: "",
					want:         false,
				},
				{
					name:         "table filter with different object filter",
					tableFilter:  "test.table1",
					objectFilter: "include-schema",
					want:         false,
					config: history.BackupConfig{
						IncludeSchemas: []string{"test"},
					},
				},
				{
					name:          "exclude table filter matches",
					tableFilter:   "test.table1",
					objectFilter:  "exclude-table",
					excludeFilter: true,
					want:          true,
					config: history.BackupConfig{
						ExcludeRelations: []string{"test.table1", "test.table2"},
					},
				},
				{
					name:          "exclude table filter with no object filter",
					tableFilter:   "test.table1",
					excludeFilter: true,
					want:          false,
				},
				{
					name:         "schema filter matches included schema",
					schemaFilter: "test",
					objectFilter: "include-schema",
					want:         true,
					config: history.BackupConfig{
						IncludeSchemas: []string{"test"},
					},
				},
				{
					name:         "schema filter with no object filter",
					schemaFilter: "test",
					want:         false,
				},
				{
					name:          "exclude schema filter matches",
					schemaFilter:  "test",
					objectFilter:  "exclude-schema",
					excludeFilter: true,
					want:          true,
					config: history.BackupConfig{
						ExcludeSchemas: []string{"test"},
					},
				},
				{
					name:          "exclude schema filter with no object filter",
					schemaFilter:  "test",
					excludeFilter: true,
					want:          false,
				},
			}
			for _, tt := range tests {
				cfg := tt.config
				got := CheckObjectFilteringExists(&cfg, tt.tableFilter, tt.schemaFilter, tt.objectFilter, tt.excludeFilter)
				Expect(got).To(Equal(tt.want), tt.name)
			}
		})
	})
})
