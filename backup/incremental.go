package backup

import (
	"fmt"
	"path"

	"github.com/apache/cloudberry-backup/history"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-backup/utils"
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/apache/cloudberry-go-libs/operating"
	"github.com/pkg/errors"
)

/*
 * Return the subset of tables whose data must be written to the current
 * incremental backup.  A table is skipped only when we can prove its data did
 * not change since the backup we are building on; any table we have no change
 * markers for (external, a storage format this version does not know how to
 * track, or a heap table without file hashes) is always included.
 *
 * Each storage format is checked on three independent signals, and any one of
 * them differing re-reads the table.  They cover each other: the content
 * signal (AO modcount, PAX aux digest) misses pure DDL, the DDL timestamp
 * misses CLUSTER because pg_stat_last_operation does not record it, and the
 * storage identity misses nothing about a rewrite but says nothing about DML.
 * Heap tables are checked as described in heapTableUnchanged.
 */
func FilterTablesForIncremental(lastBackupTOC, currentTOC *toc.TOC, lastBackupRestorePlan []history.RestorePlanEntry, tables []Table) []Table {
	// A skipped heap table's data is restored from the backup the previous
	// restore plan points it to; a table in no entry has no data to restore
	// from, e.g. it belonged to an extension, whose tables' data is not
	// backed up.
	restorableFQNs := make(map[string]bool)
	for _, entry := range lastBackupRestorePlan {
		for _, fqn := range entry.TableFQNs {
			restorableFQNs[fqn] = true
		}
	}
	var filteredTables []Table
	for _, table := range tables {
		tableFQN := table.FQN()

		if currentAOEntry, isAOTable := currentTOC.IncrementalMetadata.AO[tableFQN]; isAOTable {
			previousAOEntry := lastBackupTOC.IncrementalMetadata.AO[tableFQN]
			if previousAOEntry.Modcount != currentAOEntry.Modcount ||
				previousAOEntry.LastDDLTimestamp != currentAOEntry.LastDDLTimestamp ||
				previousAOEntry.RelFileNode != currentAOEntry.RelFileNode {
				filteredTables = append(filteredTables, table)
			}
			continue
		}

		if currentPAXEntry, isPAXTable := currentTOC.IncrementalMetadata.PAX[tableFQN]; isPAXTable {
			previousPAXEntry := lastBackupTOC.IncrementalMetadata.PAX[tableFQN]
			if previousPAXEntry.Marker != currentPAXEntry.Marker ||
				previousPAXEntry.LastDDLTimestamp != currentPAXEntry.LastDDLTimestamp ||
				previousPAXEntry.RelFileNode != currentPAXEntry.RelFileNode {
				filteredTables = append(filteredTables, table)
			}
			continue
		}

		if !restorableFQNs[tableFQN] || !heapTableUnchanged(lastBackupTOC, currentTOC, tableFQN) {
			filteredTables = append(filteredTables, table)
		}
	}

	return filteredTables
}

// heapTableUnchanged reports whether a table that is neither AO nor PAX can be
// skipped. That needs a
// file hash in both backups (recorded only with --heap-file-hash) and matching
// hashes, DDL timestamps and column hashes; anything else includes the table,
// as before.
func heapTableUnchanged(lastBackupTOC, currentTOC *toc.TOC, fqn string) bool {
	currentEntry, ok := currentTOC.IncrementalMetadata.Heap[fqn]
	if !ok || currentEntry.FileHashMD5 == "" {
		return false
	}
	previousEntry, ok := lastBackupTOC.IncrementalMetadata.Heap[fqn]
	if !ok || previousEntry.FileHashMD5 == "" {
		return false
	}
	return previousEntry.FileHashMD5 == currentEntry.FileHashMD5 &&
		previousEntry.LastDDLTimestamp == currentEntry.LastDDLTimestamp &&
		previousEntry.ColumnsMD5 == currentEntry.ColumnsMD5
}

func GetTargetBackupTimestamp() string {
	targetTimestamp := ""
	if fromTimestamp := MustGetFlagString(options.FROM_TIMESTAMP); fromTimestamp != "" {
		validateFromTimestamp(fromTimestamp)
		targetTimestamp = fromTimestamp
	} else {
		targetTimestamp = GetLatestMatchingBackupTimestamp()
	}
	return targetTimestamp
}

func GetLatestMatchingBackupTimestamp() string {
	latestTimestamp := ""
	var latestMatchingBackupHistoryEntry *history.BackupConfig

	historyDBPath := globalFPInfo.GetBackupHistoryDatabasePath()
	_, err := operating.System.Stat(historyDBPath)
	if err == nil {
		latestMatchingBackupHistoryEntry = GetLatestMatchingBackupConfig(historyDBPath, &backupReport.BackupConfig)
	}

	if latestMatchingBackupHistoryEntry == nil {
		gplog.FatalOnError(errors.Errorf("There was no matching previous backup found with the flags provided. " +
			"Please take a full backup."))
	} else {
		latestTimestamp = latestMatchingBackupHistoryEntry.Timestamp
	}

	return latestTimestamp
}

func GetLatestMatchingBackupConfig(historyDBPath string, currentBackupConfig *history.BackupConfig) *history.BackupConfig {
	// get list of timestamps for backups that match filterable flags, most recent first, then
	// iterate through them querying and checking one at a time. this is necessary due to the
	// impracticality of checking the include and exclude sets directly in a query

	historyDB, _ := history.InitializeHistoryDatabase(historyDBPath)

	whereClause := fmt.Sprintf(`backup_dir = '%s' AND database_name = '%s' AND leaf_partition_data = %v
		AND plugin = '%s' AND single_data_file = %v AND compressed = %v AND date_deleted = '' AND status = '%s'`,
		MustGetFlagString(options.BACKUP_DIR),
		currentBackupConfig.DatabaseName,
		MustGetFlagBool(options.LEAF_PARTITION_DATA),
		currentBackupConfig.Plugin,
		MustGetFlagBool(options.SINGLE_DATA_FILE),
		currentBackupConfig.Compressed,
		history.BackupStatusSucceed)

	getBackupTimetampsQuery := fmt.Sprintf(`
		SELECT timestamp
		FROM backups
		WHERE %s
		ORDER BY timestamp DESC`, whereClause)
	timestampRows, err := historyDB.Query(getBackupTimetampsQuery)
	if err != nil {
		gplog.Error("%s", err.Error())
		return nil
	}
	defer func() {
		_ = timestampRows.Close()
	}()

	timestamps := make([]string, 0)
	for timestampRows.Next() {
		var timestamp string
		err = timestampRows.Scan(&timestamp)
		if err != nil {
			gplog.Error("%s", err.Error())
			return nil
		}
		timestamps = append(timestamps, timestamp)
	}

	for _, ts := range timestamps {
		backupConfig, err := history.GetBackupConfig(ts, historyDB)
		if err != nil {
			gplog.Error("%s", err.Error())
			return nil
		}
		if !backupConfig.Failed() && matchesIncrementalFlags(backupConfig, currentBackupConfig) {
			return backupConfig
		}
	}

	return nil
}

func matchesIncrementalFlags(backupConfig *history.BackupConfig, currentBackupConfig *history.BackupConfig) bool {
	_, pluginBinaryName := path.Split(backupConfig.Plugin)
	return backupConfig.BackupDir == MustGetFlagString(options.BACKUP_DIR) &&
		backupConfig.DatabaseName == currentBackupConfig.DatabaseName &&
		backupConfig.LeafPartitionData == MustGetFlagBool(options.LEAF_PARTITION_DATA) &&
		pluginBinaryName == currentBackupConfig.Plugin &&
		backupConfig.SingleDataFile == MustGetFlagBool(options.SINGLE_DATA_FILE) &&
		backupConfig.Compressed == currentBackupConfig.Compressed &&
		// Expanding of the include list happens before this now so we must compare again current backup config
		utils.NewIncludeSet(backupConfig.IncludeRelations).Equals(utils.NewIncludeSet(currentBackupConfig.IncludeRelations)) &&
		utils.NewIncludeSet(backupConfig.IncludeSchemas).Equals(utils.NewIncludeSet(MustGetFlagStringArray(options.INCLUDE_SCHEMA))) &&
		utils.NewIncludeSet(backupConfig.ExcludeRelations).Equals(utils.NewIncludeSet(MustGetFlagStringArray(options.EXCLUDE_RELATION))) &&
		utils.NewIncludeSet(backupConfig.ExcludeSchemas).Equals(utils.NewIncludeSet(MustGetFlagStringArray(options.EXCLUDE_SCHEMA)))
}

func PopulateRestorePlan(changedTables []Table,
	restorePlan []history.RestorePlanEntry, allTables []Table) []history.RestorePlanEntry {
	currBackupRestorePlanEntry := history.RestorePlanEntry{
		Timestamp: globalFPInfo.Timestamp,
		TableFQNs: make([]string, 0, len(changedTables)),
	}

	for _, changedTable := range changedTables {
		changedTableFQN := changedTable.FQN()
		currBackupRestorePlanEntry.TableFQNs = append(currBackupRestorePlanEntry.TableFQNs, changedTableFQN)
	}

	changedTableFQNs := make(map[string]bool)
	for _, changedTable := range changedTables {
		changedTableFQN := changedTable.FQN()
		changedTableFQNs[changedTableFQN] = true
	}

	allTableFQNs := make(map[string]bool)
	for _, table := range allTables {
		tableFQN := table.FQN()
		allTableFQNs[tableFQN] = true
	}

	// Removing filtered table FQNs for the current backup from entries with previous timestamps
	for i, restorePlanEntry := range restorePlan {
		tableFQNs := make([]string, 0)
		for _, tableFQN := range restorePlanEntry.TableFQNs {
			if !changedTableFQNs[tableFQN] && allTableFQNs[tableFQN] {
				tableFQNs = append(tableFQNs, tableFQN)
			}
		}
		restorePlan[i].TableFQNs = tableFQNs
	}
	restorePlan = append(restorePlan, currBackupRestorePlanEntry)

	return restorePlan
}
