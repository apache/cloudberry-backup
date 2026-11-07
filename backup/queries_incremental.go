package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/gplog"
)

func GetAOIncrementalMetadata(connectionPool *dbconn.DBConn) map[string]toc.AOEntry {
	gplog.Verbose("Querying table row mod counts")
	var modCounts = getAllModCounts(connectionPool)
	gplog.Verbose("Querying last DDL modification timestamp for tables")
	var lastDDLTimestamps = getLastDDLTimestamps(connectionPool)
	aoTableEntries := make(map[string]toc.AOEntry)
	for aoTableFQN := range modCounts {
		aoTableEntries[aoTableFQN] = toc.AOEntry{
			Modcount:         modCounts[aoTableFQN],
			LastDDLTimestamp: lastDDLTimestamps[aoTableFQN],
		}
	}

	return aoTableEntries
}

func getAllModCounts(connectionPool *dbconn.DBConn) map[string]int64 {
	var segTableFQNs = getAOSegTableFQNs(connectionPool)
	modCounts := make(map[string]int64)
	for aoTableFQN, segTableFQN := range segTableFQNs {
		modCounts[aoTableFQN] = getModCount(connectionPool, segTableFQN)
	}
	return modCounts
}

func getAOSegTableFQNs(connectionPool *dbconn.DBConn) map[string]string {

	before7Query := fmt.Sprintf(`
		SELECT seg.aotablefqn,
			'pg_aoseg.' || quote_ident(aoseg_c.relname) AS aosegtablefqn
		FROM pg_class aoseg_c
			JOIN (SELECT pg_ao.relid AS aooid,
					pg_ao.segrelid,
					aotables.aotablefqn
				FROM pg_appendonly pg_ao
					JOIN (SELECT c.oid,
							quote_ident(n.nspname)|| '.' || quote_ident(c.relname) AS aotablefqn
						FROM pg_class c
							JOIN pg_namespace n ON c.relnamespace = n.oid
						WHERE relstorage IN ( 'ao', 'co' )
							AND %s
					) aotables ON pg_ao.relid = aotables.oid
			) seg ON aoseg_c.oid = seg.segrelid`, relationAndSchemaFilterClause())

	atLeast7Query := fmt.Sprintf(`
		SELECT seg.aotablefqn,
			'pg_aoseg.' || quote_ident(aoseg_c.relname) AS aosegtablefqn
		FROM pg_class aoseg_c
			JOIN (SELECT pg_ao.relid AS aooid,
					pg_ao.segrelid,
					aotables.aotablefqn
				FROM pg_appendonly pg_ao
					JOIN (SELECT c.oid,
							quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS aotablefqn
						FROM pg_class c
							JOIN pg_namespace n ON c.relnamespace = n.oid
							JOIN pg_am a ON c.relam = a.oid
						WHERE a.amname in ('ao_row', 'ao_column')
							AND %s
					) aotables ON pg_ao.relid = aotables.oid
			) seg ON aoseg_c.oid = seg.segrelid`, relationAndSchemaFilterClause())

	query := ""
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = before7Query
	} else {
		query = atLeast7Query
	}

	results := make([]struct {
		AOTableFQN    string
		AOSegTableFQN string
	}, 0)
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	resultMap := make(map[string]string)
	for _, result := range results {
		resultMap[result.AOTableFQN] = result.AOSegTableFQN
	}
	return resultMap
}

func getModCount(connectionPool *dbconn.DBConn, aosegtablefqn string) int64 {

	before7Query := fmt.Sprintf(`SELECT COALESCE(pg_catalog.sum(modcount), 0) AS modcount FROM %s`,
		aosegtablefqn)

	// In GPDB 7+, the coordinator no longer stores AO segment data so we must
	// query the modcount from the segments. Unfortunately, this does give a
	// false positive if a VACUUM FULL compaction happens on the AO table.
	atLeast7Query := fmt.Sprintf(`SELECT COALESCE(pg_catalog.sum(modcount), 0) AS modcount FROM gp_dist_random('%s')`,
		aosegtablefqn)

	query := ""
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = before7Query
	} else {
		query = atLeast7Query
	}

	var results []struct {
		Modcount int64
	}
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)

	return results[0].Modcount
}

func getLastDDLTimestamps(connectionPool *dbconn.DBConn) map[string]string {
	before7Query := fmt.Sprintf(`
		SELECT quote_ident(aoschema) || '.' || quote_ident(aorelname) as aotablefqn,
			lastddltimestamp
		FROM ( SELECT c.oid AS aooid,
					n.nspname AS aoschema,
					c.relname AS aorelname
				FROM pg_class c
				JOIN pg_namespace n ON c.relnamespace = n.oid
				WHERE c.relstorage IN ('ao', 'co')
				AND %s
			) aotables
		JOIN ( SELECT lo.objid,
					MAX(lo.statime) AS lastddltimestamp
				FROM pg_stat_last_operation lo
				WHERE lo.staactionname IN ('CREATE', 'ALTER', 'TRUNCATE')
				GROUP BY lo.objid
			) lastop
		ON aotables.aooid = lastop.objid`, relationAndSchemaFilterClause())

	atLeast7Query := fmt.Sprintf(`
		SELECT quote_ident(aoschema) || '.' || quote_ident(aorelname) as aotablefqn,
			lastddltimestamp
		FROM ( SELECT c.oid AS aooid,
					n.nspname AS aoschema,
					c.relname AS aorelname
				FROM pg_class c
					JOIN pg_namespace n ON c.relnamespace = n.oid
					JOIN pg_am a ON c.relam = a.oid
				WHERE a.amname in ('ao_row', 'ao_column')
					AND %s
			) aotables
		JOIN ( SELECT lo.objid,
					MAX(lo.statime) AS lastddltimestamp
				FROM pg_stat_last_operation lo
				WHERE lo.staactionname IN ('CREATE', 'ALTER', 'TRUNCATE')
				GROUP BY lo.objid
			) lastop
		ON aotables.aooid = lastop.objid`, relationAndSchemaFilterClause())

	query := ""
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = before7Query
	} else {
		query = atLeast7Query
	}

	var results []struct {
		AOTableFQN       string
		LastDDLTimestamp string
	}
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	resultMap := make(map[string]string)
	for _, result := range results {
		resultMap[result.AOTableFQN] = result.LastDDLTimestamp
	}
	return resultMap
}

// heapTable carries the catalog identity of a heap table eligible for file-hash
// incremental backup. Hashes are keyed by Oid, which is also what we pass into
// the file-hash plpgsql function (pg_relation_filepath resolves it locally on
// each segment); FQN is only for log messages, since a table may be renamed
// before the backup snapshot is taken.
type heapTable struct {
	Oid uint32
	FQN string
}

// heapFileHashBatchSize bounds the number of oids hashed per query.
const heapFileHashBatchSize = 1000

// CollectHeapFileHashes returns a per-table hash of the on-disk state of every
// heap table in the database, along with the tables it looked at. It must run
// before the backup snapshot is taken, and VerifyHeapFileHashes after it: a
// change committed after the hash was read either is flushed by the next
// backup's CHECKPOINT, moving the data files on, or, if committed before the
// snapshot, is caught by VerifyHeapFileHashes.
//
// A nil map means the hashes could not be collected; every heap table is
// then included in the next incremental, as without --heap-file-hash.
func CollectHeapFileHashes(dbName string) ([]heapTable, map[uint32]string) {
	hashConn := dbconn.NewDBConnFromEnvironment(dbName)
	hashConn.MustConnect(1)
	defer hashConn.Close()

	if !ensureFileStatFunction(hashConn) {
		return nil, nil
	}
	defer dropFileStatFunction(hashConn)

	// CHECKPOINT flushes dirty pages so pg_stat_file sees up-to-date mtime/size.
	// Without it a committed change may still sit in shared buffers, and the
	// hash would match the previous backup's.
	gplog.Verbose("Executing CHECKPOINT before heap file hash collection")
	if _, err := hashConn.Exec("CHECKPOINT;", 0); err != nil {
		gplog.Warn("CHECKPOINT failed, skipping heap file hash collection: %v", err)
		return nil, nil
	}
	// pg_stat_file reports mtime in whole seconds, so gpbackup_file_info refuses
	// to hash a file modified in the current second. Wait out the second the
	// CHECKPOINT wrote in, so only files written during collection are refused.
	time.Sleep(time.Second)

	heapTables, err := getHeapTables(hashConn, SchemaFilterClause("n"))
	if err != nil {
		gplog.Warn("Could not list heap tables, skipping heap file hash collection: %v", err)
		return nil, nil
	}
	hashes := getFileHashesForTables(hashConn, heapTables)

	// The data files also hold rows of transactions still in progress, and
	// their COMMIT only touches the commit log, not the files. Such a table
	// must not keep its hash, or the commit would never show up as a change.
	// Checking after the hashes are read and before the snapshot is enough:
	// a writer that committed in between is visible to the backup, and one
	// still in progress still holds its lock here.
	writtenOids, err := getTablesWithActiveWriters(hashConn)
	if err != nil {
		gplog.Warn("Could not check for in-progress writers, skipping heap file hash collection: %v", err)
		return nil, nil
	}
	for _, t := range heapTables {
		if _, ok := hashes[t.Oid]; ok && writtenOids[t.Oid] {
			gplog.Verbose("Not recording a file hash for %s: a transaction writing to it is in progress", t.FQN)
			delete(hashes, t.Oid)
		}
	}
	return heapTables, hashes
}

// VerifyHeapFileHashes runs after the backup snapshot is taken and keeps only
// the hashes of tables whose files did not change since CollectHeapFileHashes
// read them. A change committed after the first CHECKPOINT may still sit in
// shared buffers, invisible to that hash but visible to the snapshot; a
// second CHECKPOINT flushes it, so the table's files no longer match and its
// hash is dropped.
func VerifyHeapFileHashes(dbName string, heapTables []heapTable, hashes map[uint32]string) map[uint32]string {
	hashConn := dbconn.NewDBConnFromEnvironment(dbName)
	hashConn.MustConnect(1)
	defer hashConn.Close()

	if !ensureFileStatFunction(hashConn) {
		return nil
	}
	defer dropFileStatFunction(hashConn)

	gplog.Verbose("Executing CHECKPOINT to verify heap file hashes")
	if _, err := hashConn.Exec("CHECKPOINT;", 0); err != nil {
		gplog.Warn("CHECKPOINT failed, skipping heap file hash collection: %v", err)
		return nil
	}

	hashedTables := make([]heapTable, 0, len(hashes))
	for _, t := range heapTables {
		if _, ok := hashes[t.Oid]; ok {
			hashedTables = append(hashedTables, t)
		}
	}
	return keepUnchangedHashes(hashedTables, hashes, getFileHashesForTables(hashConn, hashedTables))
}

// keepUnchangedHashes returns the hashes of tables that current still reports
// unchanged. A table missing from current could not be re-read, so its hash
// is dropped too.
func keepUnchangedHashes(tables []heapTable, hashes, current map[uint32]string) map[uint32]string {
	verified := make(map[uint32]string, len(hashes))
	for _, t := range tables {
		hash, ok := hashes[t.Oid]
		if !ok {
			continue
		}
		if current[t.Oid] == hash {
			verified[t.Oid] = hash
		} else {
			gplog.Verbose("Not recording a file hash for %s: its files changed while the backup started", t.FQN)
		}
	}
	return verified
}

// getTablesWithActiveWriters returns the oids of relations on which another
// transaction holds a lock taken by data-modifying commands (INSERT, UPDATE,
// DELETE, COPY FROM, TRUNCATE, ...). Such a lock is held until the writing
// transaction ends.
func getTablesWithActiveWriters(hashConn *dbconn.DBConn) (map[uint32]bool, error) {
	query := `
		SELECT DISTINCT l.relation AS oid
		FROM pg_catalog.pg_locks l
		WHERE l.locktype = 'relation'
			AND l.granted
			AND l.pid IS DISTINCT FROM pg_catalog.pg_backend_pid()
			AND l.database = (SELECT oid FROM pg_catalog.pg_database WHERE datname = pg_catalog.current_database())
			AND l.mode IN ('RowExclusiveLock', 'ShareRowExclusiveLock', 'ExclusiveLock', 'AccessExclusiveLock')`

	var results []struct{ Oid uint32 }
	if err := hashConn.Select(&results, query); err != nil {
		return nil, err
	}
	oids := make(map[uint32]bool, len(results))
	for _, r := range results {
		oids[r.Oid] = true
	}
	return oids, nil
}

// ensureFileStatFunction (re)creates gp_toolkit.gpbackup_file_info, a plpgsql
// function that describes the main fork of a relation on the local segment:
// its path and the mtime+size of each of its segment files. Pure SQL, no
// plpython/shell.
//
// pg_relation_filepath only returns the first segment file; a relation larger
// than RELSEG_SIZE (1GB by default) continues in path.1, path.2, ... (see
// md.c), so the function walks them until one is missing. The path itself
// carries the relfilenode, which catches rewrites (TRUNCATE, VACUUM FULL,
// CLUSTER). The function returns an empty string when a file was modified in
// the current second, since a later write in that same second would not move
// the mtime; the caller then records no hash, forcing the table into the next
// incremental.
//
// The function is created for each phase and dropped when it ends, so a body
// left by an older gpbackup is never reused and no object is left behind in
// gp_toolkit (an extension on GPDB 7 and Cloudberry).
func ensureFileStatFunction(hashConn *dbconn.DBConn) bool {
	gplog.Verbose("Creating gp_toolkit.gpbackup_file_info function")

	// Drop the (text, text) signature from earlier versions of this feature.
	_, _ = hashConn.Exec("DROP FUNCTION IF EXISTS gp_toolkit.gpbackup_file_info(text, text);", 0)

	createSQL := `
CREATE OR REPLACE FUNCTION gp_toolkit.gpbackup_file_info(p_oid oid)
RETURNS text AS $BODY$
DECLARE
    v_base   text;
    v_path   text;
    v_mod    timestamptz;
    v_size   bigint;
    v_now    timestamptz;
    v_segno  int := 0;
    v_result text;
BEGIN
    v_base := pg_relation_filepath(p_oid);
    IF v_base IS NULL THEN
        RETURN '';
    END IF;
    v_result := v_base;
    v_now := date_trunc('second', clock_timestamp());

    LOOP
        IF v_segno = 0 THEN
            v_path := v_base;
        ELSE
            v_path := v_base || '.' || v_segno;
        END IF;

        BEGIN
            SELECT s.modification, s.size INTO v_mod, v_size FROM pg_stat_file(v_path) s;
        EXCEPTION WHEN undefined_file THEN
            EXIT;
        END;

        IF v_mod >= v_now THEN
            RETURN '';
        END IF;
        v_result := v_result || ';' || v_segno || ':' || v_mod::text || '|' || v_size;
        v_segno := v_segno + 1;
    END LOOP;

    IF v_segno = 0 THEN
        RETURN '';
    END IF;
    RETURN v_result;
END;
$BODY$ LANGUAGE plpgsql;`

	if _, err := hashConn.Exec(createSQL, 0); err != nil {
		gplog.Warn("Could not create gpbackup_file_info function, skipping heap file hash collection: %v", err)
		return false
	}
	return true
}

func dropFileStatFunction(hashConn *dbconn.DBConn) {
	if _, err := hashConn.Exec("DROP FUNCTION IF EXISTS gp_toolkit.gpbackup_file_info(oid);", 0); err != nil {
		gplog.Warn("Could not drop gpbackup_file_info function: %v", err)
	}
}

// getHeapTables returns (oid, FQN) pairs of heap tables, leaf partitions included.
func getHeapTables(conn *dbconn.DBConn, filterClause string) ([]heapTable, error) {
	amJoin, heapFilter := heapTableFilter(conn)
	query := fmt.Sprintf(`
		SELECT c.oid,
			quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS tablefqn
		FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			%s
		WHERE %s
			AND c.relkind = 'r'
			AND %s`, amJoin, heapFilter, filterClause)

	var results []struct {
		Oid      uint32
		TableFQN string
	}
	if err := conn.Select(&results, query); err != nil {
		return nil, err
	}
	out := make([]heapTable, len(results))
	for i, r := range results {
		out[i] = heapTable{Oid: r.Oid, FQN: r.TableFQN}
	}
	return out, nil
}

// getFileHashesForTables hashes gpbackup_file_info over every segment for each
// table, in batches. A table gets a hash only if every segment described its
// files; otherwise it is left out and the next incremental includes it.
func getFileHashesForTables(hashConn *dbconn.DBConn, tables []heapTable) map[uint32]string {
	result := make(map[uint32]string)
	for start := 0; start < len(tables); start += heapFileHashBatchSize {
		end := start + heapFileHashBatchSize
		if end > len(tables) {
			end = len(tables)
		}
		batch := tables[start:end]
		oids := make([]string, len(batch))
		for i, t := range batch {
			oids[i] = strconv.FormatUint(uint64(t.Oid), 10)
		}

		// gp_dist_random runs gpbackup_file_info on each segment, where
		// pg_relation_filepath resolves that segment's own copy of the table.
		query := fmt.Sprintf(`
			SELECT oid,
				md5(string_agg(gp_segment_id::text || ',' || info, chr(10) ORDER BY gp_segment_id)) AS filehash
			FROM (
				SELECT gp_segment_id, c.oid, gp_toolkit.gpbackup_file_info(c.oid) AS info
				FROM gp_dist_random('pg_class') c
				WHERE c.oid IN (%s)
			) x
			GROUP BY oid
			HAVING bool_and(COALESCE(info, '') <> '')`, strings.Join(oids, ","))

		var results []struct {
			Oid      uint32
			FileHash string
		}
		if err := hashConn.Select(&results, query); err != nil {
			gplog.Warn("Could not get file hashes for %d heap tables: %v", len(batch), err)
			continue
		}
		for _, r := range results {
			result[r.Oid] = r.FileHash
		}
	}
	return result
}

// heapIncrementalEntry is a heap table's TOC entry, without its file hash,
// along with the FQN it has in the backup snapshot.
type heapIncrementalEntry struct {
	FQN   string
	Entry toc.HeapEntry
}

// getHeapIncrementalEntries returns, for every heap table in the backup set,
// keyed by oid, what besides the file hash decides whether the table changed:
// its last DDL timestamp and a hash of its columns and of the types they use
// (see getHeapColumnHashes). A nil result means the column hashes could not
// be read and no heap table may be skipped.
func getHeapIncrementalEntries(connectionPool *dbconn.DBConn) map[uint32]heapIncrementalEntry {
	tables := getHeapLastDDLTimestamps(connectionPool)
	columnHashes, err := getHeapColumnHashes(connectionPool)
	if err != nil {
		gplog.Warn("Could not read the columns of heap tables, skipping heap file hash collection: %v", err)
		return nil
	}
	for oid, table := range tables {
		table.Entry.ColumnsMD5 = columnHashes[oid]
		tables[oid] = table
	}
	return tables
}

// heapTableFilter returns the join and the condition that select the heap
// tables eligible for file hashes from pg_class c. Unlogged tables are left
// out: a CHECKPOINT other than at shutdown does not write their buffers, so
// their files may not move when they change.
func heapTableFilter(conn *dbconn.DBConn) (amJoin string, heapFilter string) {
	if conn.Version.IsGPDB() && conn.Version.Before("6") {
		return "", "c.relstorage = 'h'"
	}
	if conn.Version.IsGPDB() && conn.Version.Before("7") {
		return "", "c.relstorage = 'h' AND c.relpersistence = 'p'"
	}
	return "JOIN pg_am a ON c.relam = a.oid", "a.amname = 'heap' AND c.relpersistence = 'p'"
}

// getHeapLastDDLTimestamps returns every heap table in the backup set with its
// last DDL timestamp (empty if none is recorded). A DDL that does not rewrite
// the table, e.g. DROP COLUMN, leaves the data files untouched, so the file
// hash alone would reuse data dumped with the old column list.
func getHeapLastDDLTimestamps(connectionPool *dbconn.DBConn) map[uint32]heapIncrementalEntry {
	amJoin, heapFilter := heapTableFilter(connectionPool)
	query := fmt.Sprintf(`
		SELECT c.oid,
			quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS tablefqn,
			COALESCE(lastop.lastddltimestamp::text, '') AS lastddltimestamp
		FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			%s
			LEFT JOIN ( SELECT lo.objid,
					MAX(lo.statime) AS lastddltimestamp
				FROM pg_stat_last_operation lo
				WHERE lo.staactionname IN ('CREATE', 'ALTER', 'TRUNCATE')
				GROUP BY lo.objid
			) lastop ON c.oid = lastop.objid
		WHERE %s
			AND c.relkind = 'r'
			AND %s`, amJoin, heapFilter, relationAndSchemaFilterClause())

	var results []struct {
		Oid              uint32
		TableFQN         string
		LastDDLTimestamp string
	}
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	resultMap := make(map[uint32]heapIncrementalEntry, len(results))
	for _, result := range results {
		resultMap[result.Oid] = heapIncrementalEntry{
			FQN:   result.TableFQN,
			Entry: toc.HeapEntry{LastDDLTimestamp: result.LastDDLTimestamp},
		}
	}
	return resultMap
}

// getHeapColumnHashes returns, keyed by oid, a hash of every heap table's
// columns (number, name, type, typmod, dropped) and of the definitions of the
// enum and composite types they use, directly or through domains, arrays,
// composite, range and multirange types: the labels of each enum and the
// attributes of each composite type. Restore loads a skipped table's old data
// file into the newest column list, so neither may change. Some changes touch
// neither the data files nor the table's DDL timestamp: ALTER TYPE ... RENAME
// VALUE or ADD/DROP ATTRIBUTE, or DROP DOMAIN ... CASCADE dropping a column.
//
// GPDB 5 cannot change types this way, so nothing is returned there. The query runs in
// the backup transaction under a savepoint, so a failure leaves the
// transaction usable and the caller can fall back to not skipping heap tables.
func getHeapColumnHashes(connectionPool *dbconn.DBConn) (map[uint32]string, error) {
	resultMap := make(map[uint32]string)
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("6") {
		return resultMap, nil
	}
	amJoin, heapFilter := heapTableFilter(connectionPool)
	// Multirange types (PostgreSQL 14) point to their range type only through
	// pg_range.rngmultitypid.
	multirangeDeps := ""
	if connectionPool.Version.IsCBDB() {
		multirangeDeps = `
			UNION ALL
			SELECT rngmultitypid, rngtypid FROM pg_range`
	}
	query := fmt.Sprintf(`
		WITH RECURSIVE type_deps(typid, deptypid) AS (
			SELECT oid, typbasetype FROM pg_type WHERE typtype = 'd'
			UNION ALL
			SELECT oid, typelem FROM pg_type WHERE typelem <> 0
			UNION ALL
			SELECT t.oid, ta.atttypid
			FROM pg_type t
				JOIN pg_attribute ta ON ta.attrelid = t.typrelid
			WHERE t.typtype = 'c' AND ta.attnum > 0 AND NOT ta.attisdropped
			UNION ALL
			SELECT rngtypid, rngsubtype FROM pg_range%s
		), column_types(relid, typid) AS (
			SELECT c.oid, ca.atttypid
			FROM pg_class c
				JOIN pg_namespace n ON c.relnamespace = n.oid
				%s
				JOIN pg_attribute ca ON ca.attrelid = c.oid
			WHERE %s
				AND c.relkind = 'r'
				AND ca.attnum > 0 AND NOT ca.attisdropped
				AND %s
			UNION
			SELECT ct.relid, d.deptypid
			FROM column_types ct
				JOIN type_deps d ON d.typid = ct.typid
		), type_defs(typid, def) AS (
			SELECT enumtypid, 'e' || enumtypid || ':' || enumsortorder || ':' || enumlabel
			FROM pg_enum
			UNION ALL
			SELECT t.oid, 'c' || t.oid || ':' || ta.attnum || ':' || ta.attname || ':' || ta.atttypid
				|| ':' || ta.atttypmod || ':' || ta.attisdropped
			FROM pg_type t
				JOIN pg_attribute ta ON ta.attrelid = t.typrelid
			WHERE t.typtype = 'c' AND ta.attnum > 0
		)
		SELECT d.relid AS oid,
			md5(string_agg(d.def, ',' ORDER BY d.def)) AS columnsmd5
		FROM (
			SELECT ct.relid, td.def
			FROM column_types ct
				JOIN type_defs td ON td.typid = ct.typid
			UNION ALL
			SELECT ca.attrelid, 'a' || ca.attnum || ':' || ca.attname || ':' || ca.atttypid
				|| ':' || ca.atttypmod || ':' || ca.attisdropped
			FROM pg_attribute ca
			WHERE ca.attnum > 0
				AND ca.attrelid IN (SELECT relid FROM column_types)
		) d
		GROUP BY d.relid`, multirangeDeps, amJoin, heapFilter, relationAndSchemaFilterClause())

	var results []struct {
		Oid        uint32
		ColumnsMD5 string
	}
	if connectionPool.Tx[0] != nil {
		connectionPool.MustExec("SAVEPOINT gpbackup_heap_columns")
		defer connectionPool.MustExec("ROLLBACK TO SAVEPOINT gpbackup_heap_columns")
	}
	if err := connectionPool.Select(&results, query); err != nil {
		return nil, err
	}
	for _, result := range results {
		resultMap[result.Oid] = result.ColumnsMD5
	}
	return resultMap, nil
}
