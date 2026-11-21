package backup

/*
 * This file contains functions related to use of synchronized database snapshots.
 */

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/jackc/pgx/v5/pgconn"
)

// CheckSnapshotTables compares the Cloudberry MVCC catalog with the current relation cache
// after ACCESS SHARE locks have been acquired. A rewrite can commit while LOCK
// waits, leaving the snapshot's old storage inaccessible. Catalog functions use
// current identity, as pg_relation_filepath does for incremental heap hashes.
func CheckSnapshotTables(conn *dbconn.DBConn, tables []Relation) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	oids := make([]string, len(tables))
	for i, table := range tables {
		oids[i] = strconv.FormatUint(uint64(table.Oid), 10)
	}
	// Include descendants even when COPY reads their data through a parent.
	query := fmt.Sprintf(`WITH RECURSIVE relations(oid) AS (
		SELECT oid FROM pg_catalog.pg_class WHERE oid IN (%s)
		UNION
		SELECT i.inhrelid FROM pg_catalog.pg_inherits i JOIN relations r ON i.inhparent = r.oid
	)
	SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS fqn,
		c.oid AS oid, c.relfilenode AS snapshotnode,
		pg_catalog.pg_relation_filenode(c.oid) AS currentnode,
		pg_catalog.to_regclass(quote_ident(n.nspname) || '.' || quote_ident(c.relname))::oid AS currentoid
	FROM relations r JOIN pg_catalog.pg_class c ON c.oid = r.oid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace`, strings.Join(oids, ","))
	var rows []struct {
		FQN          string
		Oid          uint32
		SnapshotNode uint32
		CurrentNode  sql.NullInt64
		CurrentOid   sql.NullInt64
	}
	if err := conn.Select(&rows, query); err != nil {
		return nil, fmt.Errorf("checking table storage against backup snapshot: %w", err)
	}
	conflicts := make(map[string]bool)
	for _, row := range rows {
		var reason string
		switch {
		case !row.CurrentOid.Valid || row.CurrentOid.Int64 != int64(row.Oid):
			reason = "table name no longer identifies the snapshot OID"
		case row.SnapshotNode != 0 && !row.CurrentNode.Valid:
			reason = "table storage disappeared"
		case row.SnapshotNode != 0 && row.CurrentNode.Int64 != int64(row.SnapshotNode):
			reason = "table storage changed after the snapshot"
		}
		if reason != "" {
			conflicts[fmt.Sprintf("%s (%s)", row.FQN, reason)] = true
		}
	}
	result := make([]string, 0, len(conflicts))
	for conflict := range conflicts {
		result = append(result, conflict)
	}
	sort.Strings(result)
	return result, nil
}

func prepareSnapshotTables(opts *options.Options) ([]Relation, error) {
	maxAttempts := MustGetFlagInt(options.SNAPSHOT_ATTEMPTS)
	checkSnapshot := connectionPool.Version.IsCBDB() && !MustGetFlagBool(options.METADATA_ONLY)
	for attempt := 1; ; attempt++ {
		tables := ConvertRelationsOptionsToBackup(GetIncludedUserTableRelations(connectionPool, IncludedRelationFqns))
		lockErr := LockTables(connectionPool, tables)
		var conflicts []string
		if lockErr != nil {
			var pgErr *pgconn.PgError
			if !checkSnapshot || !errors.As(lockErr, &pgErr) || (pgErr.Code != "42P01" && pgErr.Code != "3F000") {
				return nil, lockErr
			}
			conflicts = []string{lockErr.Error()}
		} else if checkSnapshot {
			var err error
			conflicts, err = CheckSnapshotTables(connectionPool, tables)
			if err != nil {
				return nil, err
			}
		}
		if len(conflicts) == 0 {
			return tables, nil
		}
		if attempt >= maxAttempts {
			return nil, fmt.Errorf("could not obtain a consistent backup snapshot after %d attempt(s): %s", attempt, strings.Join(conflicts, "; "))
		}
		gplog.Warn("Backup snapshot conflict: %s. Starting snapshot attempt %d of %d", strings.Join(conflicts, "; "), attempt+1, maxAttempts)
		if attempt == 1 {
			heapFileHashes = nil
			if MustGetFlagBool(options.HEAP_FILE_HASH) {
				gplog.Warn("Snapshot retry disables heap file hashes for this backup; all heap table data will be included and the backup may be larger")
			}
		}
		if err := restartBackupTransactions(); err != nil {
			return nil, err
		}
		if err := opts.ResetTableFilters(cmdFlags); err != nil {
			return nil, err
		}
		filterRelationClause = ""
		ValidateAndProcessFilterLists(opts)
		if err := ExpandIncludesForPartitions(connectionPool, opts, GetOidsFromRelationList(IncludedRelationFqns), cmdFlags); err != nil {
			return nil, err
		}
	}
}

func restartBackupTransactions() error {
	for connNum := 0; connNum < connectionPool.NumConns; connNum++ {
		if err := connectionPool.Rollback(connNum); err != nil {
			return fmt.Errorf("rolling back backup connection %d for snapshot retry: %w", connNum, err)
		}
	}
	return initializeBackupTransactions(globalFPInfo.Timestamp)
}

const (
	SNAPSHOT_GPDB_MIN_VERSION = "6.21.0"
)

// Export synchronized snapshot using connection 0 and return snapshotId value as string
func GetSynchronizedSnapshot(connectionPool *dbconn.DBConn) (string, error) {
	var snapshotId string
	err := connectionPool.Get(&snapshotId, "SELECT pg_catalog.pg_export_snapshot()", 0)
	if err != nil {
		return "", err
	}
	gplog.Debug("Exported synchronized snapshot %s", snapshotId)
	return snapshotId, nil
}

// Set synchronized snapshot for connNum to snapshotId
func SetSynchronizedSnapshot(connectionPool *dbconn.DBConn, connNum int, snapshotId string) error {
	if connectionPool.Tx[connNum] == nil {
		err := connectionPool.Begin(connNum) // Begins transaction in repeatable read
		if err != nil {
			return err
		}
	}
	_, err := connectionPool.Exec(fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshotId), connNum)
	if err != nil {
		return err
	}
	gplog.Debug("Worker %d: Setting synchronized snapshot to %s", connNum, snapshotId)
	return nil
}
