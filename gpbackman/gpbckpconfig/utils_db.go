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
	"database/sql"
	"fmt"
	"strings"

	"github.com/apache/cloudberry-backup/history"
)

// OpenHistoryDB opens the history backup database.
func OpenHistoryDB(historyDBPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", historyDBPath)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// GetBackupDataDB reads backup data from history database.
func GetBackupDataDB(backupName string, hDB *sql.DB) (*history.BackupConfig, error) {
	return history.GetBackupConfig(backupName, hDB)
}

// GetBackupNamesDB returns a list of backup names.
func GetBackupNamesDB(showD, showF bool, historyDB *sql.DB) ([]string, error) {
	return execQueryFunc(getBackupNameQuery(showD, showF), historyDB)
}

func GetBackupDependencies(backupName string, historyDB *sql.DB) ([]string, error) {
	return execQueryFunc(getBackupDependenciesQuery(backupName), historyDB)
}

func GetBackupNamesBeforeTimestamp(timestamp string, historyDB *sql.DB) ([]string, error) {
	return execQueryFunc(getBackupNameBeforeTimestampQuery(timestamp), historyDB)
}

func GetBackupNamesAfterTimestamp(timestamp string, historyDB *sql.DB) ([]string, error) {
	return execQueryFunc(getBackupNameAfterTimestampQuery(timestamp), historyDB)
}

func GetBackupNamesForCleanBeforeTimestamp(timestamp string, historyDB *sql.DB) ([]string, error) {
	return execQueryFunc(getBackupNameForCleanBeforeTimestampQuery(timestamp), historyDB)
}

func getBackupNameQuery(showD, showF bool) string {
	orderBy := "ORDER BY timestamp DESC;"
	getBackupsQuery := "SELECT timestamp FROM backups"
	switch {
	case showD && showF:
		getBackupsQuery = fmt.Sprintf("%s %s", getBackupsQuery, orderBy)
	case showD && !showF:
		getBackupsQuery = fmt.Sprintf("%s WHERE status != '%s' %s", getBackupsQuery, history.BackupStatusFailed, orderBy)
	case !showD && showF:
		getBackupsQuery = fmt.Sprintf("%s WHERE date_deleted IN ('', '%s', '%s', '%s') %s", getBackupsQuery, DateDeletedInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed, orderBy)
	default:
		getBackupsQuery = fmt.Sprintf("%s WHERE status != '%s' AND date_deleted IN ('', '%s', '%s', '%s') %s", getBackupsQuery, history.BackupStatusFailed, DateDeletedInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed, orderBy)
	}
	return getBackupsQuery
}

func getBackupDependenciesQuery(backupName string) string {
	return fmt.Sprintf(`
SELECT timestamp 
FROM restore_plans
WHERE timestamp != '%s'
	AND restore_plan_timestamp = '%s'
ORDER BY timestamp DESC;
`, backupName, backupName)
}

func getBackupNameBeforeTimestampQuery(timestamp string) string {
	return fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp < '%s' 
	AND status != '%s' 
	AND date_deleted IN ('', '%s', '%s') 
ORDER BY timestamp DESC;
`, timestamp, history.BackupStatusInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed)
}

func getBackupNameAfterTimestampQuery(timestamp string) string {
	return fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp > '%s' 
	AND status != '%s' 
	AND date_deleted IN ('', '%s', '%s') 
ORDER BY timestamp DESC;
`, timestamp, history.BackupStatusInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed)
}

func getBackupNameForCleanBeforeTimestampQuery(timestamp string) string {
	return fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp < '%s' 
	AND date_deleted NOT IN ('', '%s', '%s', '%s') 
ORDER BY timestamp DESC;
`, timestamp, DateDeletedPluginFailed, DateDeletedLocalFailed, DateDeletedInProgress)
}

// UpdateDeleteStatus updates the date_deleted column in the history database.
func UpdateDeleteStatus(backupName, dateDeleted string, historyDB *sql.DB) error {
	return execStatementFunc(updateDeleteStatusQuery(backupName, dateDeleted), historyDB)
}

// CleanBackupsDB cleans the backup history database.
func CleanBackupsDB(list []string, batchSize int, historyDB *sql.DB) error {
	for i := 0; i < len(list); i += batchSize {
		end := i + batchSize
		if end > len(list) {
			end = len(list)
		}
		batchIDs := list[i:end]
		idStr := "'" + strings.Join(batchIDs, "','") + "'"
		err := execStatementFunc(deleteBackupsFormTableQuery("backups", idStr), historyDB)
		if err != nil {
			return err
		}
		err = execStatementFunc(deleteBackupsFormTableQuery("restore_plans", idStr), historyDB)
		if err != nil {
			return err
		}
		err = execStatementFunc(deleteBackupsFormTableQuery("restore_plan_tables", idStr), historyDB)
		if err != nil {
			return err
		}
		err = execStatementFunc(deleteBackupsFormTableQuery("exclude_relations", idStr), historyDB)
		if err != nil {
			return err
		}
		err = execStatementFunc(deleteBackupsFormTableQuery("exclude_schemas", idStr), historyDB)
		if err != nil {
			return err
		}
		err = execStatementFunc(deleteBackupsFormTableQuery("include_relations", idStr), historyDB)
		if err != nil {
			return err
		}
		err = execStatementFunc(deleteBackupsFormTableQuery("include_schemas", idStr), historyDB)
		if err != nil {
			return err
		}
	}
	return nil
}

func deleteBackupsFormTableQuery(db, value string) string {
	return fmt.Sprintf(`DELETE FROM %s WHERE timestamp IN (%s);`, db, value)
}

func updateDeleteStatusQuery(timestamp, status string) string {
	return fmt.Sprintf(`UPDATE backups SET date_deleted = '%s' WHERE timestamp = '%s';`, status, timestamp)
}

func execQueryFunc(query string, historyDB *sql.DB) ([]string, error) {
	sqlRow, err := historyDB.Query(query)
	if err != nil {
		return nil, err
	}
	defer sqlRow.Close()
	var resultList []string
	for sqlRow.Next() {
		var b string
		err := sqlRow.Scan(&b)
		if err != nil {
			return nil, err
		}
		resultList = append(resultList, b)
	}
	if err := sqlRow.Err(); err != nil {
		return nil, err
	}
	return resultList, nil
}

func execStatementFunc(query string, historyDB *sql.DB) error {
	tx, err := historyDB.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(query)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	err = tx.Commit()
	return err
}
