package cmd

import (
	"database/sql"

	"github.com/apache/cloudberry-backup/utils"
)

type backupDeleteInterface interface {
	backupDeleteDB(backupName string, hDB *sql.DB, ignoreErrors bool) error
}

type backupPluginDeleter struct {
	pluginConfigPath string
	pluginConfig     *utils.PluginConfig
}

func (bpd *backupPluginDeleter) backupDeleteDB(backupName string, hDB *sql.DB, ignoreErrors bool) error {
	return backupDeleteDBPluginFunc(backupName, bpd.pluginConfigPath, bpd.pluginConfig, hDB, ignoreErrors)
}

type backupLocalDeleter struct {
	backupDir            string
	maxParallelProcesses int
}

func (bld *backupLocalDeleter) backupDeleteDB(backupName string, hDB *sql.DB, ignoreErrors bool) error {
	return backupDeleteDBLocalFunc(backupName, bld.backupDir, bld.maxParallelProcesses, hDB, ignoreErrors)
}
