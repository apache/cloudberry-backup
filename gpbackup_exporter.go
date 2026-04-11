//go:build gpbackup_exporter

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

package main

import (
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/apache/cloudberry-backup/exporter"
	"github.com/prometheus/client_golang/prometheus"
	version_collector "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/promslog"
	"github.com/prometheus/common/promslog/flag"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"
)

const exporterName = "gpbackup_exporter"

func main() {
	var (
		webPath = kingpin.Flag(
			"web.telemetry-path",
			"Path under which to expose metrics.",
		).Default("/metrics").String()
		webAdditionalToolkitFlags = kingpinflag.AddFlags(kingpin.CommandLine, ":19854")
		collectionInterval        = kingpin.Flag(
			"collect.interval",
			"Collecting metrics interval in seconds.",
		).Default("600").Int()
		collectionDepth = kingpin.Flag(
			"collect.depth",
			"Metrics depth collection in days. Metrics for backup older than this interval will not be collected. 0 - disable.",
		).Default("0").Int()
		gpbckpHistoryFilePath = kingpin.Flag(
			"gpbackup.history-file",
			"Path to gpbackup_history.db.",
		).Default("").String()
		gpbckpIncludeDB = kingpin.Flag(
			"gpbackup.db-include",
			"Specific db for collecting metrics. Can be specified several times.",
		).Default("").PlaceHolder("\"\"").Strings()
		gpbckpExcludeDB = kingpin.Flag(
			"gpbackup.db-exclude",
			"Specific db to exclude from collecting metrics. Can be specified several times.",
		).Default("").PlaceHolder("\"\"").Strings()
		gpbckpBackupType = kingpin.Flag(
			"gpbackup.backup-type",
			"Specific backup type for collecting metrics. One of: [full, incremental, data-only, metadata-only].",
		).Default("").String()
		gpbckpBackupCollectDeleted = kingpin.Flag(
			"gpbackup.collect-deleted",
			"Collecting metrics for deleted backups.",
		).Default("false").Bool()
		gpbckpBackupCollectFailed = kingpin.Flag(
			"gpbackup.collect-failed",
			"Collecting metrics for failed backups.",
		).Default("false").Bool()
	)
	// Set logger config.
	promslogConfig := &promslog.Config{}
	// Add flags log.level and log.format from promslog package.
	flag.AddFlags(kingpin.CommandLine, promslogConfig)
	kingpin.Version(version.Print(exporterName))
	// Add short help flag.
	kingpin.HelpFlag.Short('h')
	// Load command line arguments.
	kingpin.Parse()
	// Setup signal catching.
	sigs := make(chan os.Signal, 1)
	// Catch listed signals.
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// Set logger.
	logger := promslog.New(promslogConfig)
	// Method invoked upon seeing signal.
	go func(logger *slog.Logger) {
		s := <-sigs
		logger.Warn(
			"Stopping exporter",
			"name", filepath.Base(os.Args[0]),
			"signal", s)
		os.Exit(1)
	}(logger)
	logger.Info(
		"Starting exporter",
		"name", filepath.Base(os.Args[0]),
		"version", version.Info())
	logger.Info("Build context", "build_context", version.BuildContext())
	logger.Info(
		"History database file path",
		"file", *gpbckpHistoryFilePath)
	logger.Info(
		"Collecting metrics for deleted and failed backups",
		"deleted", *gpbckpBackupCollectDeleted,
		"failed", *gpbckpBackupCollectFailed,
	)
	if *collectionDepth > 0 {
		logger.Info(
			"Metrics depth collection in days",
			"depth", *collectionDepth)
	}
	if strings.Join(*gpbckpIncludeDB, "") != "" {
		for _, db := range *gpbckpIncludeDB {
			logger.Info(
				"Collecting metrics for specific DB",
				"DB", db)
		}
	}
	if strings.Join(*gpbckpExcludeDB, "") != "" {
		for _, db := range *gpbckpExcludeDB {
			logger.Info(
				"Exclude collecting metrics for specific DB",
				"DB", db)
		}
	}
	if *gpbckpBackupType != "" {
		logger.Info(
			"Collecting metrics for specific backup type",
			"type", *gpbckpBackupType)
	}
	// Setup parameters for exporter.
	exporter.SetPromPortAndPath(*webAdditionalToolkitFlags, *webPath)
	logger.Info(
		"Use exporter parameters",
		"endpoint", *webPath,
		"config.file", *webAdditionalToolkitFlags.WebConfigFile,
	)
	// Exporter build info metric.
	prometheus.MustRegister(version_collector.NewCollector(exporterName))
	// Start web server.
	exporter.StartPromEndpoint(version.Info(), logger)
	for {
		// Get information from gpbackup_history.db.
		exporter.GetGPBackupInfo(
			*gpbckpHistoryFilePath,
			*gpbckpBackupType,
			*gpbckpBackupCollectDeleted,
			*gpbckpBackupCollectFailed,
			*gpbckpIncludeDB,
			*gpbckpExcludeDB,
			*collectionDepth,
			logger,
		)
		// Sleep for 'collection.interval' seconds.
		time.Sleep(time.Duration(*collectionInterval) * time.Second)
	}
}
