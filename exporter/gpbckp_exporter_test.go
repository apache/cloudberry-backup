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
	"log/slog"
	"os"
	"strings"

	"github.com/apache/cloudberry-backup/history"
	"github.com/prometheus/exporter-toolkit/web"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Create a test history database from backup configs.
func fakeHistoryFileData(backupConfigs []*history.BackupConfig) (string, error) {
	tempFile, err := os.CreateTemp("", "gpbackup_history*.db")
	if err != nil {
		return "", err
	}
	tempFile.Close()
	hDB, err := history.InitializeHistoryDatabase(tempFile.Name())
	if err != nil {
		return "", err
	}
	defer hDB.Close()
	for _, config := range backupConfigs {
		err = history.StoreBackupHistory(hDB, config)
		if err != nil {
			return "", err
		}
	}
	return tempFile.Name(), nil
}

var _ = Describe("Exporter", func() {
	Describe("SetPromPortAndPath", func() {
		It("sets web flags config and endpoint", func() {
			testFlagsConfig := web.FlagConfig{
				WebListenAddresses: &([]string{":19854"}),
				WebSystemdSocket:   func(i bool) *bool { return &i }(false),
				WebConfigFile:      func(i string) *string { return &i }(""),
			}
			testEndpoint := "/metrics"
			SetPromPortAndPath(testFlagsConfig, testEndpoint)
			Expect(webFlagsConfig.WebListenAddresses).To(BeIdenticalTo(testFlagsConfig.WebListenAddresses))
			Expect(webFlagsConfig.WebSystemdSocket).To(BeIdenticalTo(testFlagsConfig.WebSystemdSocket))
			Expect(webFlagsConfig.WebConfigFile).To(BeIdenticalTo(testFlagsConfig.WebConfigFile))
			Expect(webEndpoint).To(Equal(testEndpoint))
		})
	})

	Describe("fakeHistoryFileData", func() {
		It("creates valid db from backup configs", func() {
			configs := []*history.BackupConfig{templateBackupConfig()}
			dbFile, err := fakeHistoryFileData(configs)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbFile).ToNot(BeEmpty())
			defer os.Remove(dbFile)
		})
		It("creates empty db from empty config list", func() {
			configs := []*history.BackupConfig{}
			dbFile, err := fakeHistoryFileData(configs)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbFile).ToNot(BeEmpty())
			defer os.Remove(dbFile)
		})
	})

	Describe("GetGPBackupInfo", func() {
		It("returns good data for valid backups", func() {
			metadataOnlyConfig := templateBackupConfig()
			metadataOnlyConfig.MetadataOnly = true
			metadataOnlyConfig.Timestamp = "20230118162454"
			metadataOnlyConfig.EndTime = "20230118162456"
			backupConfigs := []*history.BackupConfig{
				templateBackupConfig(),
				metadataOnlyConfig,
			}
			dbFile, err := fakeHistoryFileData(backupConfigs)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(dbFile)
			resetMetrics()
			out := &bytes.Buffer{}
			lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
				Level: slog.LevelDebug,
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			}))
			GetGPBackupInfo(dbFile, "", false, false, []string{""}, []string{""}, 0, lc)
			logOutput := out.String()
			// Metadata-only backup (later timestamp) should appear first.
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_status value=0 labels=metadata-only,test,none,none,20230118162454`))
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_deletion_status value=0 labels=metadata-only,test,none,none,none,20230118162454`))
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_info value=1 labels=/data/backups,1.30.5,metadata-only,gzip,test,6.23.0,none,none,none,20230118162454,false`))
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_duration_seconds value=2 labels=metadata-only,test,20230118162456,none,none,20230118162454`))
			// Full backup.
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_status value=0 labels=full,test,none,none,20230118152654`))
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_deletion_status value=0 labels=full,test,none,none,none,20230118152654`))
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_info value=1 labels=/data/backups,1.30.5,full,gzip,test,6.23.0,none,none,none,20230118152654,false`))
			Expect(logOutput).To(ContainSubstring(
				`level=DEBUG msg="Set up metric" metric=gpbackup_backup_duration_seconds value=2 labels=full,test,20230118152656,none,none,20230118152654`))
		})

		It("warns when no data is returned", func() {
			backupConfigs := []*history.BackupConfig{}
			dbFile, err := fakeHistoryFileData(backupConfigs)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(dbFile)
			resetMetrics()
			out := &bytes.Buffer{}
			lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
				Level: slog.LevelDebug,
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			}))
			GetGPBackupInfo(dbFile, "", false, false, []string{""}, []string{""}, 0, lc)
			Expect(out.String()).To(ContainSubstring(`No backup data returned`))
		})

		It("warns when using depth and backup is older than depth interval", func() {
			backupConfigs := []*history.BackupConfig{templateBackupConfig()}
			dbFile, err := fakeHistoryFileData(backupConfigs)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(dbFile)
			resetMetrics()
			out := &bytes.Buffer{}
			lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
				Level: slog.LevelDebug,
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			}))
			GetGPBackupInfo(dbFile, "", false, false, []string{""}, []string{""}, 14, lc)
			Expect(out.String()).To(ContainSubstring(`No succeed backups`))
		})

		It("warns when db is in both include and exclude lists", func() {
			backupConfigs := []*history.BackupConfig{templateBackupConfig()}
			dbFile, err := fakeHistoryFileData(backupConfigs)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(dbFile)
			resetMetrics()
			out := &bytes.Buffer{}
			lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
				Level: slog.LevelDebug,
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			}))
			GetGPBackupInfo(dbFile, "", false, false, []string{"test"}, []string{"test"}, 0, lc)
			Expect(out.String()).To(ContainSubstring(`DB is specified in include and exclude lists`))
			Expect(out.String()).To(ContainSubstring(`DB=test`))
		})

		It("logs errors for invalid backup values", func() {
			invalidConfig := &history.BackupConfig{
				DatabaseName:          "test",
				DataOnly:              true,
				Incremental:           true,
				IncludeSchemaFiltered: true,
				ExcludeSchemaFiltered: true,
				Timestamp:             "test",
				EndTime:               "test",
				Status:                history.BackupStatusSucceed,
			}
			backupConfigs := []*history.BackupConfig{invalidConfig}
			dbFile, err := fakeHistoryFileData(backupConfigs)
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(dbFile)
			resetMetrics()
			out := &bytes.Buffer{}
			lc := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{
				Level: slog.LevelDebug,
				ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
					if a.Key == slog.TimeKey {
						return slog.Attr{}
					}
					return a
				},
			}))
			GetGPBackupInfo(dbFile, "", false, false, []string{""}, []string{""}, 0, lc)
			logOutput := out.String()
			Expect(logOutput).To(ContainSubstring(`Parse backup timestamp value failed`))
			// Verify errors were logged (parsing errors + metric setup errors).
			errorsCount := strings.Count(logOutput, "level=ERROR")
			Expect(errorsCount).To(BeNumerically(">", 0))
		})
	})
})
