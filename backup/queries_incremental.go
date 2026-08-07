package backup

import (
	"fmt"
	"strings"

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
// incremental backup. Oid is what we pass into the file-hash plpgsql function
// (pg_relation_filepath resolves it locally on each segment); FQN is for keying
// the result map and for log messages.
type heapTable struct {
	Oid uint32
	FQN string
}

// getFileHashesForTables collects file timestamp+size MD5 hashes for the given
// heap tables using the provided connection. The connection must be reused
// across all tables so gp_segment_id mapping stays consistent.
func getFileHashesForTables(hashConn *dbconn.DBConn, tables []heapTable) map[string]string {
	result := make(map[string]string)
	for _, t := range tables {
		hash := getTableFileHash(hashConn, t.Oid, t.FQN)
		if hash != "" {
			result[t.FQN] = hash
		}
	}
	return result
}

// ensureFileStatFunction creates a plpgsql function (gp_toolkit.gpbackup_file_info)
// that uses pg_relation_filepath() + pg_stat_file() to read each segment's
// data-file mtime+size. Pure SQL, no plpython/shell. Uses a separate connection
// so a failed setup does not abort the backup transaction.
//
// pg_relation_filepath is used because manual path construction is fragile:
// custom tablespaces live under pg_tblspc/<tsp>/PG_<major>_<catver>/<db>/<rfn>,
// and that PG_<major>_<catver> segment varies by server version. Letting the
// server compute the path keeps us correct across versions and tablespace
// configurations.
func ensureFileStatFunction(connectionPool *dbconn.DBConn) bool {
	gplog.Verbose("Setting up file hash detection function (plpgsql + pg_stat_file)")

	setupConn := dbconn.NewDBConnFromEnvironment(connectionPool.DBName)
	setupConn.MustConnect(1)
	defer setupConn.Close()

	checkSQL := `SELECT 1 AS val FROM pg_proc
		WHERE proname = 'gpbackup_file_info'
		AND pronargs = 1
		AND pronamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'gp_toolkit');`
	var checkResult []struct{ Val int }
	err := setupConn.Select(&checkResult, checkSQL)

	if err != nil || len(checkResult) == 0 {
		gplog.Verbose("Creating gp_toolkit.gpbackup_file_info function")

		// Drop any older (text, text) signature from a previous gpbackup version
		// before recreating with the new (oid) signature.
		_, _ = setupConn.Exec("DROP FUNCTION IF EXISTS gp_toolkit.gpbackup_file_info(text, text);", 0)

		createSQL := `
CREATE OR REPLACE FUNCTION gp_toolkit.gpbackup_file_info(p_oid oid)
RETURNS text AS $BODY$
DECLARE
    v_path text;
    v_mod  text;
    v_size text;
BEGIN
    v_path := pg_relation_filepath(p_oid);
    IF v_path IS NULL THEN
        RETURN '';
    END IF;

    BEGIN
        SELECT (pg_stat_file(v_path)).modification::text,
               (pg_stat_file(v_path)).size::text
        INTO v_mod, v_size;
    EXCEPTION WHEN OTHERS THEN
        RETURN '';
    END;

    RETURN COALESCE(v_mod, '') || '|' || COALESCE(v_size, '0');
END;
$BODY$ LANGUAGE plpgsql;`

		_, createErr := setupConn.Exec(createSQL, 0)
		if createErr != nil {
			gplog.Warn("Could not create gpbackup_file_info function: %v", createErr)
			return false
		}
		gplog.Verbose("gpbackup_file_info function created successfully")
	}

	return true
}

// getHeapTables returns (oid, FQN) pairs of heap tables eligible for incremental backup.
func getHeapTables(connectionPool *dbconn.DBConn) []heapTable {
	var query string
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = fmt.Sprintf(`
			SELECT c.oid,
				quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS tablefqn
			FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			WHERE c.relstorage = 'h'
			AND c.relkind = 'r'
			AND c.oid NOT IN (SELECT inhrelid FROM pg_inherits)
			AND %s`, relationAndSchemaFilterClause())
	} else {
		query = fmt.Sprintf(`
			SELECT c.oid,
				quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS tablefqn
			FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			JOIN pg_am a ON c.relam = a.oid
			WHERE a.amname = 'heap'
			AND c.relkind = 'r'
			AND c.oid NOT IN (SELECT inhrelid FROM pg_inherits)
			AND %s`, relationAndSchemaFilterClause())
	}

	var results []struct {
		Oid      uint32
		TableFQN string
	}
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	out := make([]heapTable, len(results))
	for i, r := range results {
		out[i] = heapTable{Oid: r.Oid, FQN: r.TableFQN}
	}
	return out
}

// getTableFileHash computes an MD5 hash of per-segment data-file mtime+size for
// a heap table, using gp_toolkit.gpbackup_file_info(oid). gp_dist_random('gp_id')
// runs the function locally on each segment; pg_relation_filepath on the segment
// resolves the local path, so each segment hashes its own copy of the table.
func getTableFileHash(hashConn *dbconn.DBConn, oid uint32, fqn string) string {
	// oid is interpolated as an integer literal -- no string-escaping concerns.
	query := fmt.Sprintf(`
		SELECT COALESCE(md5(string_agg(
			gp_segment_id::text || ',' || info, chr(10) ORDER BY gp_segment_id
		)), '') AS filehash
		FROM (
			SELECT gp_segment_id,
				gp_toolkit.gpbackup_file_info(%d::oid) AS info
			FROM gp_dist_random('gp_id')
		) x
		WHERE info <> ''`, oid)

	var results []struct{ FileHash string }
	err := hashConn.Select(&results, query)
	if err != nil {
		gplog.Warn("Could not get file hash for %s: %v", fqn, err)
		return ""
	}
	if len(results) == 0 || results[0].FileHash == "" {
		return ""
	}
	return results[0].FileHash
}

// GetAOContentHashes returns a per-table aoseg content hash for every AO/AOCS table.
// In GP5, parent modcount changes when any child partition is modified, but each
// child's aoseg table only changes when that specific child receives data — so
// hashing the aoseg rows gives partition-level granularity.
// Uses a dedicated connection to avoid transaction abort propagation.
func GetAOContentHashes(connectionPool *dbconn.DBConn) map[string]string {
	segTableFQNs := getAOSegTableFQNs(connectionPool)

	hashConn := dbconn.NewDBConnFromEnvironment(connectionPool.DBName)
	hashConn.MustConnect(1)
	defer hashConn.Close()

	result := make(map[string]string)
	for aoTableFQN, aosegTableFQN := range segTableFQNs {
		hash := getAOSegContentHash(hashConn, aosegTableFQN)
		if hash != "" {
			result[aoTableFQN] = hash
		}
	}
	return result
}

// getAOSegContentHash hashes content-bearing columns of the aoseg metadata table —
// deliberately excluding modcount, which in GP5 propagates across sibling partitions
// even when only one is modified.
//
//   - AO row (pg_aoseg_*): segno + eof + tupcount.
//   - AOCS column store (pg_aocsseg_*): layout differs by product / version.
//     Cloudberry AOCS exposes vpinfo (vertical-partition info, a bytea containing
//     per-column eofs); GPDB 6+ exposes column_num + physical_segno +
//     eof_uncompressed; pre-GP6 only has segno + tupcount. If a column is missing
//     on a given version, the query fails and getAOSegContentHash returns "",
//     and FilterTablesForIncremental falls back to modcount comparison for that
//     table.
func getAOSegContentHash(hashConn *dbconn.DBConn, aosegTableFQN string) string {
	isColumnStore := strings.Contains(aosegTableFQN, "pg_aocsseg")

	var cols string
	if isColumnStore {
		switch {
		case !hashConn.Version.IsGPDB():
			// Cloudberry AOCS: segno, tupcount, vpinfo (bytea with per-column EOFs)
			cols = "segno::text || ',' || tupcount::text || ',' || encode(vpinfo, 'hex')"
		case hashConn.Version.Before("6"):
			cols = "segno::text || ',' || tupcount::text"
		default:
			// GPDB 6+ AOCS
			cols = "segno::text || ',' || column_num::text || ',' || physical_segno::text || ',' || tupcount::text || ',' || eof_uncompressed::text"
		}
	} else {
		cols = "segno::text || ',' || eof::text || ',' || tupcount::text"
	}

	var query string
	if hashConn.Version.IsGPDB() && hashConn.Version.Before("7") {
		query = fmt.Sprintf(`SELECT COALESCE(md5(string_agg(%s,
			chr(10) ORDER BY segno)), '') AS contenthash FROM %s`, cols, aosegTableFQN)
	} else {
		query = fmt.Sprintf(`SELECT COALESCE(md5(string_agg(
			gp_segment_id::text || ',' || %s,
			chr(10) ORDER BY gp_segment_id, segno)), '') AS contenthash FROM gp_dist_random('%s')`,
			cols, aosegTableFQN)
	}

	var results []struct{ ContentHash string }
	err := hashConn.Select(&results, query)
	if err != nil {
		gplog.Warn("Could not get aoseg content hash for %s: %v", aosegTableFQN, err)
		return ""
	}
	if len(results) == 0 {
		return ""
	}
	return results[0].ContentHash
}
