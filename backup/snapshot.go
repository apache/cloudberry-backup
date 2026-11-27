package backup

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/gplog"
	"github.com/jackc/pgx/v5/pgconn"
)

// CheckSnapshotTableLocks finds relations in the backup snapshot without a
// lock, including descendants read through a parent.
func CheckSnapshotTableLocks(conn *dbconn.DBConn, tables []Relation) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	conflicts := make(map[string]bool)
	const batchSize = 1000
	for start := 0; start < len(tables); start += batchSize {
		end := start + batchSize
		if end > len(tables) {
			end = len(tables)
		}
		oids := make([]string, end-start)
		for i, table := range tables[start:end] {
			oids[i] = strconv.FormatUint(uint64(table.Oid), 10)
		}
		query := fmt.Sprintf(`WITH RECURSIVE relations(oid) AS (
		SELECT oid FROM pg_catalog.pg_class WHERE oid IN (%s)
		UNION
		SELECT i.inhrelid FROM pg_catalog.pg_inherits i JOIN relations r ON i.inhparent = r.oid
	)
	SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS fqn
	FROM relations r JOIN pg_catalog.pg_class c ON c.oid = r.oid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE NOT EXISTS (
		SELECT 1 FROM pg_catalog.pg_locks l
		WHERE l.locktype = 'relation' AND l.relation = c.oid
			AND l.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database())
			AND l.pid = pg_catalog.pg_backend_pid()
			AND l.mode = 'AccessShareLock' AND l.granted
	)`, strings.Join(oids, ","))
		var rows []struct{ FQN string }
		if err := conn.Select(&rows, query); err != nil {
			return nil, fmt.Errorf("checking backup table locks: %w", err)
		}
		for _, row := range rows {
			conflicts[row.FQN] = true
		}
	}
	result := make([]string, 0, len(conflicts))
	for conflict := range conflicts {
		result = append(result, conflict)
	}
	sort.Strings(result)
	return result, nil
}

func snapshotPartitionParents(conn *dbconn.DBConn, tables []Relation, missingLocksOnly bool) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	oids := make([]string, len(tables))
	for i, table := range tables {
		oids[i] = strconv.FormatUint(uint64(table.Oid), 10)
	}
	// Start at selected roots, but include subparents reached by COPY even if
	// a filter omitted them from the explicit table list.
	const query = `WITH RECURSIVE parents(oid, depth) AS (
		SELECT c.oid, 0 FROM pg_catalog.pg_class c
		WHERE c.oid = ANY($1::oid[]) AND c.relkind = 'p' AND NOT c.relispartition
		UNION ALL
		SELECT c.oid, p.depth + 1 FROM parents p
		JOIN pg_catalog.pg_inherits i ON i.inhparent = p.oid
		JOIN pg_catalog.pg_class c ON c.oid = i.inhrelid
		WHERE c.relkind = 'p' AND c.relispartition
	)
	SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname)
	FROM parents p JOIN pg_catalog.pg_class c ON c.oid = p.oid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE NOT $2::boolean OR NOT EXISTS (
		SELECT 1 FROM pg_catalog.pg_locks l
		WHERE l.locktype = 'relation' AND l.relation = p.oid
			AND l.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = current_database())
			AND l.pid = pg_catalog.pg_backend_pid()
			AND l.mode = 'ShareUpdateExclusiveLock' AND l.granted
	)
	ORDER BY p.depth, p.oid`
	var parents []string
	if err := conn.SelectWithArgs(&parents, query, "{"+strings.Join(oids, ",")+"}", missingLocksOnly); err != nil {
		return nil, fmt.Errorf("checking partition parents for backup: %w", err)
	}
	return parents, nil
}

func lockSnapshotPartitionParents(conn *dbconn.DBConn, parents []string) error {
	for batch := range slices.Chunk(parents, 100) {
		names := "ONLY " + strings.Join(batch, ", ONLY ")
		if err := lockBackupTables(conn, names, "IN SHARE UPDATE EXCLUSIVE MODE"); err != nil {
			return fmt.Errorf("locking partition parents %s in SHARE UPDATE EXCLUSIVE mode: %w", names, err)
		}
	}
	return nil
}

// PrepareBackupSnapshot locks Cloudberry tables before exporting the shared
// snapshot. The connection pool must have no open transactions on entry.
func PrepareBackupSnapshot(opts *options.Options) error {
	maxAttempts := MustGetFlagInt(options.SNAPSHOT_ATTEMPTS)
	copyThroughParent := !MustGetFlagBool(options.METADATA_ONLY) && !MustGetFlagBool(options.LEAF_PARTITION_DATA)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := resolveSnapshotFilters(opts); err != nil {
			return err
		}
		tables := ConvertRelationsOptionsToBackup(GetIncludedUserTableRelations(connectionPool, IncludedRelationFqns))
		var parents []string
		if copyThroughParent {
			var err error
			parents, err = snapshotPartitionParents(connectionPool, tables, false)
			if err != nil {
				return err
			}
		}
		// A SELECT before LOCK would establish the transaction snapshot too early.
		if err := connectionPool.Begin(0); err != nil {
			return err
		}
		// ATTACH takes a lock on its parent that's compatible with ACCESS SHARE.
		lockErr := lockSnapshotPartitionParents(connectionPool, parents)
		if lockErr == nil {
			lockErr = LockTables(connectionPool, tables)
		}
		var conflicts []string
		if lockErr != nil {
			var pgErr *pgconn.PgError
			if !errors.As(lockErr, &pgErr) || (pgErr.Code != "42P01" && pgErr.Code != "3F000") {
				return lockErr
			}
			conflicts = []string{lockErr.Error()}
		} else {
			var err error
			backupSnapshot, err = GetSynchronizedSnapshot(connectionPool)
			if err != nil {
				return err
			}
			if err = resolveSnapshotFilters(opts); err != nil {
				return err
			}
			tables = ConvertRelationsOptionsToBackup(GetIncludedUserTableRelations(connectionPool, IncludedRelationFqns))
			// Locking new tables here would reopen the concurrent TRUNCATE race.
			conflicts, err = CheckSnapshotTableLocks(connectionPool, tables)
			if err != nil {
				return err
			}
			if copyThroughParent && len(conflicts) == 0 {
				conflicts, err = snapshotPartitionParents(connectionPool, tables, true)
				if err != nil {
					return err
				}
			}
		}
		if len(conflicts) == 0 {
			for connNum := 1; connNum < connectionPool.NumConns; connNum++ {
				if err := SetSynchronizedSnapshot(connectionPool, connNum, backupSnapshot); err != nil {
					return err
				}
			}
			return nil
		}
		if err := connectionPool.Rollback(0); err != nil {
			return fmt.Errorf("rolling back backup snapshot preparation: %w", err)
		}
		backupSnapshot = ""
		details := snapshotConflictSummary(conflicts)
		if attempt >= maxAttempts {
			return fmt.Errorf("could not lock the backup table set before taking a snapshot after %d attempt(s): %s", attempt, details)
		}
		gplog.Warn("Backup table set changed: %s. Starting snapshot attempt %d of %d", details, attempt+1, maxAttempts)
	}
	return fmt.Errorf("snapshot preparation requires at least one attempt")
}

func resolveSnapshotFilters(opts *options.Options) error {
	if err := opts.ResetTableFilters(cmdFlags); err != nil {
		return err
	}
	filterRelationClause = ""
	ValidateAndProcessFilterLists(opts)
	return ExpandIncludesForPartitions(connectionPool, opts, GetOidsFromRelationList(IncludedRelationFqns), cmdFlags)
}

func snapshotConflictSummary(conflicts []string) string {
	const limit = 10
	if len(conflicts) > limit {
		return fmt.Sprintf("%s; and %d more", strings.Join(conflicts[:limit], "; "), len(conflicts)-limit)
	}
	return strings.Join(conflicts, "; ")
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
