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
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/history"
	"github.com/prometheus/client_golang/prometheus"
)

const emptyLabel = "none"

type setUpMetricValueFunType func(metric *prometheus.GaugeVec, value float64, labels ...string) error

type backupMap map[string]time.Time
type lastBackupMap map[string]backupMap
type dbStatusMap map[string]bool

func setUpMetricValue(metric *prometheus.GaugeVec, value float64, labels ...string) error {
	metricVec, err := metric.GetMetricWithLabelValues(labels...)
	if err != nil {
		return err
	}
	// The situation should be handled by the prometheus libraries.
	// But, anything is possible.
	if metricVec == nil {
		err := errors.New("metric is nil")
		return err
	}
	metricVec.Set(value)
	return nil
}

// Get status code about backup deletion status.
// Based on available statuses from gpbackman utility documentation,
// but not limited to that.
//   - 0 - backup still exists;
//   - 1 - backup was successfully deleted;
//   - 2 - the deletion is in progress;
//   - 3 - last delete attempt failed to delete backup from plugin storage;
//   - 4 - last delete attempt failed to delete backup from local storage;
func getDeletedStatusCode(valueDateDeleted string) (string, float64) {
	var (
		dateDeleted   string
		deletedStatus float64
	)
	switch valueDateDeleted {
	case "":
		dateDeleted = emptyLabel
		deletedStatus = 0
	case gpbckpconfig.DateDeletedInProgress:
		dateDeleted = emptyLabel
		deletedStatus = 2
	case gpbckpconfig.DateDeletedPluginFailed:
		dateDeleted = emptyLabel
		deletedStatus = 3
	case gpbckpconfig.DateDeletedLocalFailed:
		dateDeleted = emptyLabel
		deletedStatus = 4
	default:
		dateDeleted = valueDateDeleted
		deletedStatus = 1
	}
	return dateDeleted, deletedStatus
}

// Reset all metrics.
func resetMetrics() {
	resetBackupMetrics()
	resetLastBackupMetrics()
	resetExporterMetrics()
}

func setUpMetric(metric *prometheus.GaugeVec, metricName string, value float64, setUpMetricValueFun setUpMetricValueFunType, logger *slog.Logger, labels ...string) {
	logger.Debug(
		"Set up metric",
		"metric", metricName,
		"value", value,
		"labels", strings.Join(labels, ","),
	)
	err := setUpMetricValueFun(metric, value, labels...)
	if err != nil {
		logger.Error(
			"Metric set up failed",
			"metric", metricName,
			"err", err,
		)
	}
}

func dbInList(db string, list []string) bool {
	if listEmpty(list) {
		return false
	}
	for _, val := range list {
		if val == db {
			return true
		}
	}
	return false
}

// Check list not empty.
func listEmpty(list []string) bool {
	return strings.Join(list, "") == ""
}

// Get and parse data from history database:
//   - file with extension .db (sqlite).
//
// Returns parsed data or error.
func parseBackupData(historyFile string, collectDeleted, collectFailed bool, logger *slog.Logger) ([]*history.BackupConfig, error) {
	if filepath.Ext(historyFile) != ".db" {
		return nil, errors.New("file has an extension other than db (sqlite)")
	}
	return getDataFromHistoryDB(historyFile, collectDeleted, collectFailed, logger)
}

func getDataFromHistoryDB(historyFile string, collectDeleted, collectFailed bool, logger *slog.Logger) ([]*history.BackupConfig, error) {
	hDB, err := gpbckpconfig.OpenHistoryDB(historyFile)
	if err != nil {
		logger.Error("Open gpbackup history db failed", "err", err)
		return nil, err
	}
	defer func() {
		errClose := hDB.Close()
		if errClose != nil {
			logger.Error("Close gpbackup history db failed", "err", errClose)
		}
	}()
	backupList, err := gpbckpconfig.GetBackupNamesDB(collectDeleted, collectFailed, hDB)
	if err != nil {
		logger.Error("Get backups from history db failed", "err", err)
		return nil, err
	}
	// Get data for selected backups.
	var backupConfigs []*history.BackupConfig
	for _, backupName := range backupList {
		backupData, err := gpbckpconfig.GetBackupDataDB(backupName, hDB)
		if err != nil {
			logger.Error("Get backup data from history db failed", "err", err)
			return nil, err
		}
		backupConfigs = append(backupConfigs, backupData)
	}
	return backupConfigs, nil
}
