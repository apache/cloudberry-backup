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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/apache/cloudberry-backup/history"
)

// OpenHistoryDB opens an existing gpbackup_history.db SQLite database.
//
// The path is opened with the SQLite "rw" URI mode so that a missing file
// produces a clear error rather than being silently created as an empty
// database (which would later fail with a confusing "no such table: backups"
// when callers issue queries). Existence is also pre-checked with os.Stat to
// surface a friendly error message that points the caller at the relevant
// flag and environment variables.
func OpenHistoryDB(historyDBPath string) (*sql.DB, error) {
	if _, err := os.Stat(historyDBPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf(
				"gpbackup history database file not found: %s. "+
					"Specify the path via --history-db, run gpbackman from "+
					"the directory that contains gpbackup_history.db, or "+
					"pass --auto-load-history-db to resolve it from "+
					"$COORDINATOR_DATA_DIRECTORY",
				historyDBPath,
			)
		}
		return nil, fmt.Errorf("stat history db %q: %w", historyDBPath, err)
	}
	// mode=rw opens an existing database for read+write but never creates one.
	db, err := sql.Open("sqlite3", "file:"+historyDBPath+"?mode=rw")
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

func GetBackupNamesBeforeTimestamp(timestamp, databaseName string, historyDB *sql.DB) ([]string, error) {
	return execBackupNamesQuery(getBackupNameBeforeTimestampQuery(timestamp, databaseName), databaseName, historyDB)
}

func GetBackupNamesAfterTimestamp(timestamp, databaseName string, historyDB *sql.DB) ([]string, error) {
	return execBackupNamesQuery(getBackupNameAfterTimestampQuery(timestamp, databaseName), databaseName, historyDB)
}

func GetBackupNamesForCleanBeforeTimestamp(timestamp, databaseName string, historyDB *sql.DB) ([]string, error) {
	return execBackupNamesQuery(getBackupNameForCleanBeforeTimestampQuery(timestamp, databaseName), databaseName, historyDB)
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

func getBackupNameBeforeTimestampQuery(timestamp, databaseName string) string {
	query := fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp < '%s' 
	AND status != '%s' 
	AND date_deleted IN ('', '%s', '%s') 
`, timestamp, history.BackupStatusInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed)
	return addDatabaseNamePredicate(query, databaseName) + "ORDER BY timestamp DESC;\n"
}

func getBackupNameAfterTimestampQuery(timestamp, databaseName string) string {
	query := fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp > '%s' 
	AND status != '%s' 
	AND date_deleted IN ('', '%s', '%s') 
`, timestamp, history.BackupStatusInProgress, DateDeletedPluginFailed, DateDeletedLocalFailed)
	return addDatabaseNamePredicate(query, databaseName) + "ORDER BY timestamp DESC;\n"
}

func getBackupNameForCleanBeforeTimestampQuery(timestamp, databaseName string) string {
	query := fmt.Sprintf(`
SELECT timestamp 
FROM backups 
WHERE timestamp < '%s' 
	AND date_deleted NOT IN ('', '%s', '%s', '%s') 
`, timestamp, DateDeletedPluginFailed, DateDeletedLocalFailed, DateDeletedInProgress)
	return addDatabaseNamePredicate(query, databaseName) + "ORDER BY timestamp DESC;\n"
}

func addDatabaseNamePredicate(query, databaseName string) string {
	if databaseName == "" {
		return query
	}
	return query + "\tAND database_name = ?\n"
}

func execBackupNamesQuery(query, databaseName string, historyDB *sql.DB) ([]string, error) {
	if databaseName == "" {
		return execQueryFunc(query, historyDB)
	}
	return execQueryFunc(query, historyDB, databaseName)
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

func execQueryFunc(query string, historyDB *sql.DB, args ...any) ([]string, error) {
	sqlRow, err := historyDB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = sqlRow.Close()
	}()
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
