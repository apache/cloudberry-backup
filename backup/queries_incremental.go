package backup

import (
	"fmt"

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
