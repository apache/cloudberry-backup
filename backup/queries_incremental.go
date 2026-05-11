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

// getFileHashesForTables collects file timestamp+size MD5 hashes for a list of tables
// using the provided connection. The connection must be reused across all tables
// to ensure consistent gp_segment_id mapping.
func getFileHashesForTables(hashConn *dbconn.DBConn, tableFQNs []string) map[string]string {
	result := make(map[string]string)
	for _, fqn := range tableFQNs {
		hash := getTableFileHash(hashConn, fqn)
		if hash != "" {
			result[fqn] = hash
		}
	}
	return result
}

// ensureFileStatFunction creates a plpgsql function (gp_toolkit.gpbackup_file_info)
// that uses pg_stat_file() to read each segment's data-file mtime+size. Pure SQL,
// no plpython/shell. Uses a separate connection so a failed setup does not abort
// the backup transaction.
func ensureFileStatFunction(connectionPool *dbconn.DBConn) bool {
	gplog.Verbose("Setting up file hash detection function (plpgsql + pg_stat_file)")

	setupConn := dbconn.NewDBConnFromEnvironment(connectionPool.DBName)
	setupConn.MustConnect(1)
	defer setupConn.Close()

	checkSQL := `SELECT 1 AS val FROM pg_proc
		WHERE proname = 'gpbackup_file_info'
		AND pronamespace = (SELECT oid FROM pg_namespace WHERE nspname = 'gp_toolkit');`
	var checkResult []struct{ Val int }
	err := setupConn.Select(&checkResult, checkSQL)

	if err != nil || len(checkResult) == 0 {
		gplog.Verbose("Creating gp_toolkit.gpbackup_file_info function")

		createSQL := `
CREATE OR REPLACE FUNCTION gp_toolkit.gpbackup_file_info(p_schema text, p_table text)
RETURNS text AS $BODY$
DECLARE
    v_tsp  oid;
    v_rfn  oid;
    v_dboid oid;
    v_dbtsp oid;
    v_path text;
    v_mod  text;
    v_size text;
BEGIN
    SELECT c.reltablespace, c.relfilenode INTO v_tsp, v_rfn
    FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid
    WHERE n.nspname = p_schema AND c.relname = p_table;

    IF v_rfn IS NULL THEN
        RETURN '';
    END IF;

    SELECT oid, dattablespace INTO v_dboid, v_dbtsp
    FROM pg_database WHERE datname = current_database();

    IF v_tsp = 0 THEN
        v_tsp := v_dbtsp;
    END IF;

    IF v_tsp = 1663 THEN
        v_path := 'base/' || v_dboid || '/' || v_rfn;
    ELSE
        v_path := 'pg_tblspc/' || v_tsp || '/' || v_dboid || '/' || v_rfn;
    END IF;

    BEGIN
        SELECT (pg_stat_file(v_path)).modification::text,
               (pg_stat_file(v_path)).size::text
        INTO v_mod, v_size;
    EXCEPTION WHEN OTHERS THEN
        v_mod := '';
        v_size := '0';
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

// getHeapTableFQNs returns FQNs of heap tables eligible for incremental backup.
func getHeapTableFQNs(connectionPool *dbconn.DBConn) []string {
	var query string
	if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
		query = fmt.Sprintf(`
			SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS tablefqn
			FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			WHERE c.relstorage = 'h'
			AND c.relkind = 'r'
			AND c.oid NOT IN (SELECT inhrelid FROM pg_inherits)
			AND %s`, relationAndSchemaFilterClause())
	} else {
		query = fmt.Sprintf(`
			SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS tablefqn
			FROM pg_class c
			JOIN pg_namespace n ON c.relnamespace = n.oid
			JOIN pg_am a ON c.relam = a.oid
			WHERE a.amname = 'heap'
			AND c.relkind = 'r'
			AND c.oid NOT IN (SELECT inhrelid FROM pg_inherits)
			AND %s`, relationAndSchemaFilterClause())
	}

	var results []struct{ TableFQN string }
	err := connectionPool.Select(&results, query)
	gplog.FatalOnError(err)
	fqns := make([]string, len(results))
	for i, r := range results {
		fqns[i] = r.TableFQN
	}
	return fqns
}

// getTableFileHash computes an MD5 hash of per-segment data-file mtime+size for a
// heap table, using gp_toolkit.gpbackup_file_info (plpgsql + pg_stat_file).
// gp_dist_random('gp_id') runs the function locally on each segment.
func getTableFileHash(hashConn *dbconn.DBConn, tableFQN string) string {
	parts := splitFQN(tableFQN)
	if len(parts) != 2 {
		return ""
	}
	schema, table := parts[0], parts[1]

	query := fmt.Sprintf(`
		SELECT COALESCE(md5(string_agg(
			gp_segment_id::text || ',' || info, chr(10) ORDER BY gp_segment_id
		)), '') AS filehash
		FROM (
			SELECT gp_segment_id,
				gp_toolkit.gpbackup_file_info('%s', '%s') AS info
			FROM gp_dist_random('gp_id')
		) x
		WHERE info <> ''`,
		schema, table)

	var results []struct{ FileHash string }
	err := hashConn.Select(&results, query)
	if err != nil {
		gplog.Warn("Could not get file hash for %s: %v", tableFQN, err)
		return ""
	}
	if len(results) == 0 || results[0].FileHash == "" {
		return ""
	}
	return results[0].FileHash
}

// splitFQN splits "schema.table" into [schema, table], stripping quote chars.
func splitFQN(fqn string) []string {
	fqn = strings.ReplaceAll(fqn, "\"", "")
	parts := strings.SplitN(fqn, ".", 2)
	return parts
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
//     per-column eofs); GP7+ exposes column_num + physical_segno + eof_uncompressed;
//     pre-GP6 only has segno + tupcount.
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
			// GP7+ AOCS
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
