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
	"errors"
	"log/slog"
	"os"
	"time"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/history"
	"github.com/prometheus/client_golang/prometheus"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
}

func fakeSetUpMetricValue(_ *prometheus.GaugeVec, _ float64, _ ...string) error {
	return errors.New("custom error for test")
}

// Create a SQLite database file with missing tables.
func createCorruptedDBFile() string {
	tempFile, err := os.CreateTemp("", "test_corrupted_*.db")
	Expect(err).ToNot(HaveOccurred())
	defer tempFile.Close()
	return tempFile.Name()
}

// Create a database with invalid backup name.
func createDBWithInvalidBackupName() string {
	tempFile, err := os.CreateTemp("", "test_invalid_backup_*.db")
	Expect(err).ToNot(HaveOccurred())
	defer tempFile.Close()
	db, err := gpbckpconfig.OpenHistoryDB(tempFile.Name())
	Expect(err).ToNot(HaveOccurred())
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS backups (
		timestamp TEXT PRIMARY KEY,
		date_deleted TEXT,
		database_name TEXT,
		status TEXT
	)`)
	Expect(err).ToNot(HaveOccurred())
	// Insert a backup with an invalid timestamp.
	_, err = db.Exec(`INSERT INTO backups (timestamp, date_deleted, database_name, status) VALUES
		('invalid_backup_name', '', 'testdb', 'Success')`)
	Expect(err).ToNot(HaveOccurred())
	return tempFile.Name()
}

func templateBackupConfig() *history.BackupConfig {
	return &history.BackupConfig{
		BackupDir:             "/data/backups",
		BackupVersion:         "1.30.5",
		Compressed:            true,
		CompressionType:       "gzip",
		DatabaseName:          "test",
		DatabaseVersion:       "6.23.0",
		DataOnly:              false,
		DateDeleted:           "",
		ExcludeRelations:      []string{},
		ExcludeSchemaFiltered: false,
		ExcludeSchemas:        []string{},
		ExcludeTableFiltered:  false,
		IncludeRelations:      []string{},
		IncludeSchemaFiltered: false,
		IncludeSchemas:        []string{},
		IncludeTableFiltered:  false,
		Incremental:           false,
		LeafPartitionData:     false,
		MetadataOnly:          false,
		Plugin:                "",
		PluginVersion:         "",
		RestorePlan:           []history.RestorePlanEntry{},
		SingleDataFile:        false,
		Timestamp:             "20230118152654",
		EndTime:               "20230118152656",
		WithoutGlobals:        false,
		WithStatistics:        false,
		Status:                history.BackupStatusSucceed,
	}
}

func templateUnixTime() int64 {
	// Thu Jan 18 2023 20:00:00 UTC
	var curUnixTime int64 = 1674072000
	return curUnixTime
}

func returnTimeTime(sTime string) time.Time {
	rTime, err := time.Parse(gpbckpconfig.Layout, sTime)
	if err != nil {
		panic(err)
	}
	return rTime
}

var _ = Describe("Parser", func() {
	Describe("getDeletedStatusCode", func() {
		It("returns 0 for existing backup (empty date)", func() {
			dateDeleted, status := getDeletedStatusCode("")
			Expect(dateDeleted).To(Equal("none"))
			Expect(status).To(Equal(float64(0)))
		})
		It("returns 2 for In Progress", func() {
			dateDeleted, status := getDeletedStatusCode(gpbckpconfig.DateDeletedInProgress)
			Expect(dateDeleted).To(Equal("none"))
			Expect(status).To(Equal(float64(2)))
		})
		It("returns 3 for Plugin Backup Delete Failed", func() {
			dateDeleted, status := getDeletedStatusCode(gpbckpconfig.DateDeletedPluginFailed)
			Expect(dateDeleted).To(Equal("none"))
			Expect(status).To(Equal(float64(3)))
		})
		It("returns 4 for Local Delete Failed", func() {
			dateDeleted, status := getDeletedStatusCode(gpbckpconfig.DateDeletedLocalFailed)
			Expect(dateDeleted).To(Equal("none"))
			Expect(status).To(Equal(float64(4)))
		})
		It("returns 1 for valid deletion date", func() {
			dateDeleted, status := getDeletedStatusCode("20230118150331")
			Expect(dateDeleted).To(Equal("20230118150331"))
			Expect(status).To(Equal(float64(1)))
		})
	})

	Describe("setUpMetricValue", func() {
		It("returns error when labels don't match", func() {
			err := setUpMetricValue(gpbckpExporterStatusMetric, 0, "demo", "bad")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("dbInList", func() {
		It("returns true when db is in list", func() {
			Expect(dbInList("test", []string{"test"})).To(BeTrue())
		})
		It("returns false when db is not in list", func() {
			Expect(dbInList("test", []string{"demo"})).To(BeFalse())
		})
		It("returns false for empty list", func() {
			Expect(dbInList("test", []string{""})).To(BeFalse())
		})
	})

	Describe("listEmpty", func() {
		It("returns true for empty list", func() {
			Expect(listEmpty([]string{})).To(BeTrue())
		})
		It("returns false for non-empty list", func() {
			Expect(listEmpty([]string{"a", "b", "c"})).To(BeFalse())
		})
	})

	Describe("parseBackupData", func() {
		It("returns error for yaml file", func() {
			tempFile, err := os.CreateTemp("", "test*.yaml")
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(tempFile.Name())
			got, err := parseBackupData(tempFile.Name(), false, false, getLogger())
			Expect(err).To(HaveOccurred())
			Expect(got).To(BeNil())
		})
		It("returns error for empty db file", func() {
			tempFile, err := os.CreateTemp("", "test*.db")
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(tempFile.Name())
			got, err := parseBackupData(tempFile.Name(), false, false, getLogger())
			Expect(err).To(HaveOccurred())
			Expect(got).To(BeNil())
		})
		It("returns error for unknown file extension", func() {
			tempFile, err := os.CreateTemp("", "test*.txt")
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(tempFile.Name())
			got, err := parseBackupData(tempFile.Name(), false, false, getLogger())
			Expect(err).To(HaveOccurred())
			Expect(got).To(BeNil())
		})
	})

	Describe("getDataFromHistoryDB", func() {
		It("returns error for invalid db file path", func() {
			out := &bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelError}))
			_, err := getDataFromHistoryDB("/nonexistent/path/to/db.db", false, false, logger)
			Expect(err).To(HaveOccurred())
			Expect(out.String()).To(ContainSubstring("Get backups from history db failed"))
		})
		It("returns error for corrupted db with invalid backup data", func() {
			dbFile := createCorruptedDBFile()
			defer os.Remove(dbFile)
			out := &bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelError}))
			_, err := getDataFromHistoryDB(dbFile, false, false, logger)
			Expect(err).To(HaveOccurred())
			Expect(out.String()).To(ContainSubstring("Get backups from history db failed"))
		})
		It("returns error for db with invalid backup name", func() {
			dbFile := createDBWithInvalidBackupName()
			defer os.Remove(dbFile)
			out := &bytes.Buffer{}
			logger := slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: slog.LevelError}))
			_, err := getDataFromHistoryDB(dbFile, false, false, logger)
			Expect(err).To(HaveOccurred())
			Expect(out.String()).To(ContainSubstring("Get backup data from history db failed"))
		})
	})
})
