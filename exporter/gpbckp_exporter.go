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
	"net/http"
	"os"
	"time"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/history"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/exporter-toolkit/web"
)

var (
	webFlagsConfig web.FlagConfig
	webEndpoint    string
)

// SetPromPortAndPath sets HTTP endpoint parameters.
func SetPromPortAndPath(flagsConfig web.FlagConfig, endpoint string) {
	webFlagsConfig = flagsConfig
	webEndpoint = endpoint
}

// StartPromEndpoint run HTTP endpoint.
func StartPromEndpoint(version string, logger *slog.Logger) {
	go func(logger *slog.Logger) {
		if webEndpoint == "" {
			logger.Error("Metric endpoint is empty; aborting exporter startup", "endpoint", webEndpoint)
			os.Exit(1)
		}
		http.Handle(webEndpoint, promhttp.Handler())
		if webEndpoint != "/" {
			landingConfig := web.LandingConfig{
				Name:        "cloudberry-backup exporter",
				Description: "Prometheus exporter for Apache Cloudberry (Incubating) backup utility",
				HeaderColor: "#476b6b",
				Version:     version,
				Profiling:   "false",
				Links: []web.LandingLinks{
					{
						Address: webEndpoint,
						Text:    "Metrics",
					},
				},
			}
			landingPage, err := web.NewLandingPage(landingConfig)
			if err != nil {
				logger.Error("Error creating landing page", "err", err)
				os.Exit(1)
			}
			http.Handle("/", landingPage)
		}
		server := &http.Server{
			ReadHeaderTimeout: 5 * time.Second,
		}
		if err := web.ListenAndServe(server, &webFlagsConfig, logger); err != nil {
			logger.Error("Run web endpoint failed", "err", err)
			os.Exit(1)
		}
	}(logger)
}

// GetGPBackupInfo get and parse gpbackup history database.
func GetGPBackupInfo(historyFile, backupType string, collectDeleted, collectFailed bool, dbInclude, dbExclude []string, collectDepth int, logger *slog.Logger) {
	// To calculate the time elapsed since the last completed backup for specific database.
	// For all databases values are calculated relative to one value.
	// To calculate the time elapsed since the last completed backup for specific database.
	// For all databases values are calculated relative to one value.
	currentTime := time.Now()
	currentUnixTime := currentTime.Unix()
	// Calculate metrics collection depth.
	// For backups with timestamp older than this - metrics doesn't collect.
	collectDepthTime := currentTime.AddDate(0, 0, -collectDepth)
	// The backup number can be reduced using filters for deleted and failed backups.
	backupConfigs, err := parseBackupData(historyFile, collectDeleted, collectFailed, logger)
	if err != nil {
		logger.Error("Get data failed", "err", err)
	}
	// Reset metrics.
	resetMetrics()
	if len(backupConfigs) != 0 {
		// Like lastbackups["testDB"]["full"] = time
		lastBackups := make(lastBackupMap)
		dbStatus := make(dbStatusMap)
		for i := 0; i < len(backupConfigs); i++ {
			db := backupConfigs[i].DatabaseName
			// If the same database is specified in include and exclude list,
			// then metrics for this database will not be collected.
			if !dbInList(db, dbExclude) {
				if listEmpty(dbInclude) || dbInList(db, dbInclude) {
					dbStatus[db] = true
					bckpType, err := gpbckpconfig.GetBackupType(backupConfigs[i])
					if err != nil {
						logger.Error("Parse backup type value failed", "err", err)
					}
					// Check backup type and compare with backup type filter.
					if backupType == "" || backupType == bckpType {
						// History file contains backup timestamp and endtime with timezone information.
						// It is necessary to take this into account when calculating time intervals.
						// With a high probability, the exporter will work in the same timezone as Greenplum cluster.
						// If this is not the case, then there are many questions about the backup process.
						bckpStartTime, err := time.ParseInLocation(gpbckpconfig.Layout, backupConfigs[i].Timestamp, time.Local)
						if err != nil {
							logger.Error("Parse backup timestamp value failed", "err", err)
						}
						bckpStopTime, err := time.ParseInLocation(gpbckpconfig.Layout, backupConfigs[i].EndTime, time.Local)
						if err != nil {
							logger.Error("Parse backup end time value failed", "err", err)
						}
						// Only if set correct value for collectDepth.
						if collectDepth > 0 {
							// gpbackup_history.db is sorted by timestamp values.
							// The data of the most recent backup is always located at the beginning.
							// So as soon as we get the first value that is older than collectDepthTime,
							// the cycle can be broken.
							// If this behavior ever changes, then this code needs to be refactored.
							if collectDepthTime.Before(bckpStartTime) {
								getBackupMetrics(backupConfigs[i], setUpMetricValue, logger)
							} else {
								break
							}
						} else {
							getBackupMetrics(backupConfigs[i], setUpMetricValue, logger)
						}
						if backupConfigs[i].Status == history.BackupStatusSucceed {
							// Check specific database key already exist.
							if dbLastBackups, ok := lastBackups[db]; ok {
								// Check specific backup type key already exist.
								if _, ok := dbLastBackups[bckpType]; !ok {
									dbLastBackups[bckpType] = bckpStopTime
								}
								// A small note on the code above.
								// Since the history file is already sorted, the first occurrence will be the last backup.
								// However, if sorting is suddenly removed in the future, the code should be something like this:
								//	if curLastTime, ok := dbLastBackups[bckpType]; ok {
								//		if curLastTime.Before(bckpStopTime) {
								//			dbLastBackups[bckpType] = bckpStopTime
								//		}
								//	} else {
								//		dbLastBackups[bckpType] = bckpStopTime
								//	}
							} else {
								lastBackups[db] = backupMap{bckpType: bckpStopTime}
							}
						}
					}
				}
			} else if dbInList(db, dbInclude) {
				// When db is specified in both include and exclude lists, a warning is displayed in the log
				// and data for this db is not collected.
				// Set zero metric value for this db.
				dbStatus[db] = false
				logger.Warn("DB is specified in include and exclude lists", "DB", db)
			}
		}
		if len(lastBackups) != 0 {
			getBackupLastMetrics(lastBackups, currentUnixTime, setUpMetricValue, logger)
		} else {
			logger.Warn("No succeed backups")
		}
		getExporterStatusMetrics(dbStatus, setUpMetricValue, logger)
	} else {
		logger.Warn("No backup data returned")
	}
}
