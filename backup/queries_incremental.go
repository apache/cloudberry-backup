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
	gplog.Verbose("Querying storage identity for tables")
	var relFileNodes = getAORelFileNodes(connectionPool)
	aoTableEntries := make(map[string]toc.AOEntry)
	for aoTableFQN := range modCounts {
		aoTableEntries[aoTableFQN] = toc.AOEntry{
			LastDDLTimestamp: lastDDLTimestamps[aoTableFQN],
			Modcount:         modCounts[aoTableFQN],
			RelFileNode:      relFileNodes[aoTableFQN],
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
	// query the modcount from the segments.
	//
	// The modcount alone cannot tell a rewritten table from an untouched one.
	// Any rewrite moves the table onto a fresh pg_aoseg relation whose rows
	// were written by a single statement, so the sum restarts at the segment
	// count, which is also what a bulk-loaded table reads. getAORelFileNodes()
	// is what distinguishes the two.
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

	atLeast7Query := lastDDLTimestampQuery("'ao_row', 'ao_column'")

	query := ""
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = before7Query
	} else {
		query = atLeast7Query
	}

	return selectLastDDLTimestamps(connectionPool, query)
}

/*
 * Build the GPDB7+/Cloudberry query returning the most recent CREATE, ALTER or
 * TRUNCATE timestamp for every table using one of the given access methods.
 * amNames is a ready-quoted SQL list, e.g. "'ao_row', 'ao_column'".
 */
func lastDDLTimestampQuery(amNames string) string {
	return fmt.Sprintf(`
		SELECT quote_ident(aoschema) || '.' || quote_ident(aorelname) as aotablefqn,
			lastddltimestamp
		FROM ( SELECT c.oid AS aooid,
					n.nspname AS aoschema,
					c.relname AS aorelname
				FROM pg_class c
					JOIN pg_namespace n ON c.relnamespace = n.oid
					JOIN pg_am a ON c.relam = a.oid
				WHERE a.amname in (%s)
					AND %s
			) aotables
		JOIN ( SELECT lo.objid,
					MAX(lo.statime) AS lastddltimestamp
				FROM pg_stat_last_operation lo
				WHERE lo.staactionname IN ('CREATE', 'ALTER', 'TRUNCATE')
				GROUP BY lo.objid
			) lastop
		ON aotables.aooid = lastop.objid`, amNames, relationAndSchemaFilterClause())
}

func selectLastDDLTimestamps(connectionPool *dbconn.DBConn, query string) map[string]string {
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

	// The column hashes (see getHeapColumnHashes) need a recursive query.
	if hashConn.Version.IsGPDB() && hashConn.Version.Before("6") {
		gplog.Warn("--heap-file-hash requires Greenplum 6 or later, skipping heap file hash collection")
		return nil, nil
	}

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
		columnsMD5, ok := columnHashes[oid]
		if !ok {
			delete(tables, oid)
			continue
		}
		table.Entry.ColumnsMD5 = columnsMD5
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
// Tables that use, anywhere in that closure, a type whose values are dumped as
// the names of catalog objects they reference by oid (regclass and the other
// reg* types, aclitem) get no hash, and so are never skipped: renaming or
// dropping the referenced object changes what COPY writes, or makes the old
// value fail to load, and nothing here records it. Tables without columns get
// no hash either.
//
// The query runs in the backup transaction under a savepoint, so a failure
// leaves the transaction usable and the caller can fall back to not skipping
// heap tables.
func getHeapColumnHashes(connectionPool *dbconn.DBConn) (map[uint32]string, error) {
	resultMap := make(map[uint32]string)
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
			SELECT enumtypid, 'e' || enumtypid::text || ':' || enumsortorder || ':' || enumlabel
			FROM pg_enum
			UNION ALL
			SELECT t.oid, 'c' || t.oid::text || ':' || ta.attnum || ':' || ta.attname || ':' || ta.atttypid
				|| ':' || ta.atttypmod || ':' || ta.attisdropped
			FROM pg_type t
				JOIN pg_attribute ta ON ta.attrelid = t.typrelid
			WHERE t.typtype = 'c' AND ta.attnum > 0
		)
		SELECT d.relid AS oid,
			md5(string_agg(d.def, ',' ORDER BY d.def)) AS columnsmd5,
			d.relid IN (
				SELECT ct.relid
				FROM column_types ct
					JOIN pg_type t ON t.oid = ct.typid
					JOIN pg_namespace tn ON tn.oid = t.typnamespace
				WHERE tn.nspname = 'pg_catalog'
					AND t.typname IN ('regproc', 'regprocedure', 'regoper', 'regoperator',
						'regclass', 'regtype', 'regconfig', 'regdictionary', 'regnamespace',
						'regrole', 'regcollation', 'aclitem')
			) AS refsobjects
		FROM (
			SELECT ct.relid, td.def
			FROM column_types ct
				JOIN type_defs td ON td.typid = ct.typid
			UNION ALL
			SELECT ca.attrelid, 'a' || ca.attnum::text || ':' || ca.attname || ':' || ca.atttypid
				|| ':' || ca.atttypmod || ':' || ca.attisdropped
			FROM pg_attribute ca
			WHERE ca.attnum > 0
				AND ca.attrelid IN (SELECT relid FROM column_types)
		) d
		GROUP BY d.relid`, multirangeDeps, amJoin, heapFilter, relationAndSchemaFilterClause())

	var results []struct {
		Oid         uint32
		ColumnsMD5  string
		RefsObjects bool
	}
	if connectionPool.Tx[0] != nil {
		connectionPool.MustExec("SAVEPOINT gpbackup_heap_columns")
		defer connectionPool.MustExec("ROLLBACK TO SAVEPOINT gpbackup_heap_columns")
	}
	if err := connectionPool.Select(&results, query); err != nil {
		return nil, err
	}
	for _, result := range results {
		if !result.RefsObjects {
			resultMap[result.Oid] = result.ColumnsMD5
		}
	}
	return resultMap, nil
}

/*
 * PAX incremental metadata.
 *
 * PAX tables have no pg_aoseg entry and therefore no modcount.  What they do
 * have is an aux relation in the pg_ext_aux namespace describing their
 * micro-partitions, and Cloudberry builds PAX in one of two mutually exclusive
 * layouts:
 *
 *   - manifest layout (cmake USE_MANIFEST_API): pg_ext_aux.pg_manifest_<oid>,
 *     a single-row table whose "path" column names the relation's current
 *     manifest file.  Any transaction that dirties the manifest flushes it
 *     under a freshly generated name and rewrites that row.
 *   - catalog layout (cmake USE_PAX_CATALOG): pg_ext_aux.pg_pax_blocks_<oid>,
 *     one row per micro-partition.  Inserts add rows; deletes write a new
 *     visimap file and update the row's ptvisimapname to its name.
 *
 * Which layout a cluster uses is fixed at compile time, so we detect it by
 * aux relation name rather than by version.  Either way the aux relation is
 * reduced to one fixed-width digest, because FilterTablesForIncremental only
 * ever compares a table's marker against the previous backup's marker for
 * equality, and a marker is retained for every PAX table in the backup set.
 *
 * Like the AO modcount, this over-reports rather than under-reports: a rewrite
 * that preserves the data (VACUUM FULL, CLUSTER) changes the digest and causes
 * an unnecessary re-backup of the table.  A read-only transaction does not
 * dirty the aux relation, so plain queries never trigger one.
 */

const (
	paxAuxManifest = "manifest"
	paxAuxBlocks   = "blocks"
)

type paxAuxRelation struct {
	PAXTableFQN string
	AuxTableFQN string
	AuxKind     string
}

func GetPAXIncrementalMetadata(connectionPool *dbconn.DBConn) map[string]toc.PAXEntry {
	paxTableEntries := make(map[string]toc.PAXEntry)

	// PAX is a Cloudberry table access method; it never exists on GPDB.
	if connectionPool.Version.IsGPDB() {
		return paxTableEntries
	}

	gplog.Verbose("Querying PAX table aux metadata markers")
	markers := getAllPAXMarkers(connectionPool)
	if len(markers) == 0 {
		return paxTableEntries
	}

	gplog.Verbose("Querying last DDL modification timestamp for PAX tables")
	lastDDLTimestamps := getPAXLastDDLTimestamps(connectionPool)
	gplog.Verbose("Querying storage identity for PAX tables")
	relFileNodes := getPAXRelFileNodes(connectionPool)
	for paxTableFQN := range markers {
		paxTableEntries[paxTableFQN] = toc.PAXEntry{
			LastDDLTimestamp: lastDDLTimestamps[paxTableFQN],
			Marker:           markers[paxTableFQN],
			RelFileNode:      relFileNodes[paxTableFQN],
		}
	}

	return paxTableEntries
}

func getAllPAXMarkers(connectionPool *dbconn.DBConn) map[string]string {
	var auxRelations = getPAXAuxRelations(connectionPool)
	markers := make(map[string]string, len(auxRelations))
	for _, auxRelation := range auxRelations {
		markers[auxRelation.PAXTableFQN] = getPAXMarker(connectionPool, auxRelation)
	}
	return markers
}

/*
 * Pair every PAX table in the backup set with its aux relation.  The root of a
 * partitioned PAX table has no aux relation of its own and so is absent from
 * the result, exactly as an AO root is absent from the modcount map.
 */
func getPAXAuxRelations(connectionPool *dbconn.DBConn) []paxAuxRelation {
	query := fmt.Sprintf(`
		SELECT paxtables.paxtablefqn,
			quote_ident(aux_n.nspname) || '.' || quote_ident(aux_c.relname) AS auxtablefqn,
			CASE WHEN aux_c.relname::text = 'pg_manifest_' || paxtables.oid::text
				THEN '%s' ELSE '%s' END AS auxkind
		FROM pg_class aux_c
			JOIN pg_namespace aux_n ON aux_c.relnamespace = aux_n.oid
			JOIN (SELECT c.oid,
					quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS paxtablefqn
				FROM pg_class c
					JOIN pg_namespace n ON c.relnamespace = n.oid
					JOIN pg_am a ON c.relam = a.oid
				WHERE a.amname = 'pax'
					AND %s
			) paxtables
			ON aux_c.relname::text IN ('pg_manifest_' || paxtables.oid::text,
				'pg_pax_blocks_' || paxtables.oid::text)
		WHERE aux_n.nspname = 'pg_ext_aux'
			AND aux_c.relkind = 'r'`,
		paxAuxManifest, paxAuxBlocks, relationAndSchemaFilterClause())

	results := make([]paxAuxRelation, 0)
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	return results
}

/*
 * Reduce one PAX aux relation to a single digest.  Aux relations only hold
 * rows on the segments, so they are read through gp_dist_random.
 *
 * The digest is always keyed by gp_segment_id.  Block numbers are assigned
 * per segment, so the same ptblockname occurs once per segment with unrelated
 * contents; folding the segments together would let two different cluster
 * states reduce to the same multiset of rows.
 *
 * SHA-256 rather than MD5: the dangerous direction here is two *different*
 * aux states hashing alike, because that makes a changed table look unchanged
 * and silently drops its data from the backup.  MD5 has constructible
 * collisions, so it is the wrong primitive for a check whose failure mode is
 * data loss, and the hash is far too cheap for its strength to be worth
 * trading away.  Collisions are not merely unlikely under SHA-256, there is
 * no known way to produce one.
 *
 * Hashing at all, rather than keeping the aux rows themselves, is deliberate:
 * a marker is held in memory and written to the TOC for every PAX table in
 * the backup set, so it has to be a fixed width independent of how many
 * micro-partitions a table has.
 */
func getPAXMarker(connectionPool *dbconn.DBConn, auxRelation paxAuxRelation) string {
	var query string
	if auxRelation.AuxKind == paxAuxManifest {
		// Exactly one row per segment, so a single pass is already bounded.
		query = fmt.Sprintf(`
		SELECT COALESCE(pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(
				pg_catalog.string_agg(gp_segment_id::text || ':' || path,
					',' ORDER BY gp_segment_id, path), 'UTF8')), 'hex'), '') AS marker
		FROM gp_dist_random('%s')`, auxRelation.AuxTableFQN)
	} else {
		// One row per micro-partition, so digest each segment's rows on that
		// segment and only combine the per-segment digests on the
		// coordinator.  That keeps coordinator memory proportional to the
		// number of segments rather than to the size of the table.
		//
		// ptblockname and pttupcount cover inserts, ptblocksize covers
		// rewrites that preserve the row count, and ptvisimapname covers
		// deletes, which write a new visimap file without adding a
		// micro-partition.  ptstatistics is deliberately left out: it is a
		// composite type that only ever changes alongside one of these
		// columns.  ptblockname is the aux relation's primary key, so
		// ordering by it alone is deterministic within a segment.
		query = fmt.Sprintf(`
		SELECT COALESCE(pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(
				pg_catalog.string_agg(segment_digest, ',' ORDER BY gp_segment_id), 'UTF8')), 'hex'), '') AS marker
		FROM (SELECT gp_segment_id,
				gp_segment_id::text || ':' || pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(
					pg_catalog.string_agg(ptblockname::text || ':' || pttupcount::text || ':' ||
						ptblocksize::text || ':' || COALESCE(ptvisimapname::text, ''),
						',' ORDER BY ptblockname), 'UTF8')), 'hex') AS segment_digest
			FROM gp_dist_random('%s')
			GROUP BY gp_segment_id) segments`, auxRelation.AuxTableFQN)
	}

	var results []struct {
		Marker string
	}
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)

	return results[0].Marker
}

func getPAXLastDDLTimestamps(connectionPool *dbconn.DBConn) map[string]string {
	return selectLastDDLTimestamps(connectionPool, lastDDLTimestampQuery("'pax'"))
}

/*
 * Storage identity: each table's relfilenode.
 *
 * A rewrite -- CLUSTER, or any of the ALTER forms that rebuild a table --
 * replaces the table's storage, and the relfilenode is the only thing that
 * reliably says so.  The modcount cannot: a rewrite moves the table onto a
 * fresh pg_aoseg whose rows were written by one statement, so the sum restarts
 * at the segment count, indistinguishable from a bulk-loaded table that has
 * not been touched since.  Once ordinary DML brings the counter back up to the
 * value the previous backup recorded, the table looks unchanged and is
 * skipped.  The DDL timestamp does not cover this either: CLUSTER,
 * SET UNLOGGED and SET ACCESS METHOD all rewrite the table without being
 * recorded in pg_stat_last_operation.
 *
 * This reads the coordinator's relfilenode, not the segments'.  The two are
 * allocated independently and never match -- a table reading 28848 on the
 * coordinator reads 60115 on every segment -- but they move together, because
 * a rewrite is a dispatched DDL that replaces the relation on every node in
 * one distributed transaction.  Measured across CLUSTER, TRUNCATE,
 * SET WITH (reorganize=true), SET DISTRIBUTED BY, ALTER COLUMN TYPE,
 * SET UNLOGGED and SET ACCESS METHOD, the two sides changed together every
 * time, and VACUUM FULL left both alone.
 *
 * Reading the coordinator keeps this to one local catalog query instead of a
 * gp_dist_random scan of pg_class on every segment, and avoids depending on an
 * ordered aggregate surviving a motion.  Partitioned roots appear here with a
 * relfilenode of 0; they have no pg_aoseg and no aux relation, so they are
 * never looked up.
 *
 * It costs nothing in false positives.  Measured on both AO layouts, the
 * relfilenode is left alone by INSERT, UPDATE, DELETE, VACUUM and VACUUM FULL,
 * and changes only where the storage is genuinely replaced.
 */
func getAORelFileNodes(connectionPool *dbconn.DBConn) map[string]int64 {
	before7Query := fmt.Sprintf(`
		SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS aotablefqn,
			c.relfilenode::int8 AS relfilenode
		FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.relstorage IN ('ao', 'co')
			AND %s`, relationAndSchemaFilterClause())

	atLeast7Query := relFileNodeQuery("'ao_row', 'ao_column'")

	query := ""
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = before7Query
	} else {
		query = atLeast7Query
	}

	return selectRelFileNodes(connectionPool, query)
}

func getPAXRelFileNodes(connectionPool *dbconn.DBConn) map[string]int64 {
	return selectRelFileNodes(connectionPool, relFileNodeQuery("'pax'"))
}

/*
 * Build the GPDB7+/Cloudberry query returning the relfilenode of every table
 * using one of the given access methods.  amNames is a ready-quoted SQL list,
 * e.g. "'ao_row', 'ao_column'".
 */
func relFileNodeQuery(amNames string) string {
	return fmt.Sprintf(`
		SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS aotablefqn,
			c.relfilenode::int8 AS relfilenode
		FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_am a ON a.oid = c.relam
		WHERE a.amname in (%s)
			AND %s`, amNames, relationAndSchemaFilterClause())
}

func selectRelFileNodes(connectionPool *dbconn.DBConn, query string) map[string]int64 {
	var results []struct {
		AOTableFQN  string
		RelFileNode int64
	}
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	resultMap := make(map[string]int64, len(results))
	for _, result := range results {
		resultMap[result.AOTableFQN] = result.RelFileNode
	}
	return resultMap
}
