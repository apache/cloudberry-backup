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

package end_to_end_test

import (
	"fmt"

	"github.com/apache/cloudberry-backup/history"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*
 * A matrix of every table access method Cloudberry can store user data in,
 * crossed with the three shapes a table can take, backed up and restored both
 * ways round.
 *
 *                 plain      uniform-AM partitioned   leaf of a mixed-AM
 *                                                     partitioned table
 *   heap            x                 x                       x
 *   ao_row          x                 x                       x
 *   ao_column       x                 x                       x
 *   pax             x                 x                       x
 *
 * The mixed-AM table is the interesting shape: its leaves each use a different
 * access method, so one backup set exercises every data path at once and the
 * root carries no access method of its own.  Classic Greenplum partition
 * syntax cannot express it, because per-partition WITH (appendonly=...) has no
 * spelling for pax, so it is built with declarative partitioning while the
 * uniform tables use the classic syntax.  That way both syntaxes are covered.
 *
 * The incremental tests assert which tables land in each restore plan entry,
 * not just that the data survives.  That is where the access methods actually
 * differ: heap has no change detection and is re-backed-up unconditionally,
 * while AO and PAX tables are only re-read when gpbackup can see that they
 * changed.
 */

const (
	matrixSchema      = "matrix"
	matrixLeaves      = 3
	matrixRowsPerLeaf = 100
)

/*
 * pax is registered by a contrib module, so build the matrix out of whatever
 * the cluster under test actually has rather than skipping the whole thing.
 */
func matrixAccessMethods() []string {
	accessMethods := []string{"heap", "ao_row", "ao_column"}
	paxCount := dbconn.MustSelectString(backupConn,
		"SELECT count(*) AS string FROM pg_am WHERE amname = 'pax'")
	if paxCount != "0" {
		accessMethods = append(accessMethods, "pax")
	}
	return accessMethods
}

// The tables the matrix compares across a restore: the roots, not the leaves.
// Reading a root reads every leaf under it.
func matrixTables(accessMethods []string) []string {
	tables := make([]string, 0, 2*len(accessMethods)+1)
	for _, am := range accessMethods {
		tables = append(tables,
			fmt.Sprintf("%s.%s_plain", matrixSchema, am),
			fmt.Sprintf("%s.%s_part", matrixSchema, am))
	}
	return append(tables, fmt.Sprintf("%s.mixed_part", matrixSchema))
}

func createMatrixSchema(conn *dbconn.DBConn, accessMethods []string) {
	testhelper.AssertQueryRuns(conn, fmt.Sprintf(
		"DROP SCHEMA IF EXISTS %s CASCADE; CREATE SCHEMA %s", matrixSchema, matrixSchema))

	for _, am := range accessMethods {
		testhelper.AssertQueryRuns(conn, fmt.Sprintf(
			"CREATE TABLE %s.%s_plain (id int, part int, v text) USING %s DISTRIBUTED BY (id)",
			matrixSchema, am, am))
		testhelper.AssertQueryRuns(conn, fmt.Sprintf(
			`CREATE TABLE %s.%s_part (id int, part int, v text) USING %s DISTRIBUTED BY (id)
				PARTITION BY RANGE (part) (START (0) END (%d) EVERY (1))`,
			matrixSchema, am, am, matrixLeaves))
	}

	testhelper.AssertQueryRuns(conn, fmt.Sprintf(
		"CREATE TABLE %s.mixed_part (id int, part int, v text) DISTRIBUTED BY (id) PARTITION BY RANGE (part)",
		matrixSchema))
	for i, am := range accessMethods {
		testhelper.AssertQueryRuns(conn, fmt.Sprintf(
			"CREATE TABLE %s.mixed_part_%s PARTITION OF %s.mixed_part FOR VALUES FROM (%d) TO (%d) USING %s",
			matrixSchema, am, matrixSchema, i, i+1, am))
	}
}

func populateMatrixSchema(conn *dbconn.DBConn, accessMethods []string) {
	rows := matrixLeaves * matrixRowsPerLeaf
	for _, am := range accessMethods {
		for _, table := range []string{am + "_plain", am + "_part"} {
			testhelper.AssertQueryRuns(conn, fmt.Sprintf(
				"INSERT INTO %s.%s SELECT i, i %% %d, 'r' || i FROM generate_series(1, %d) i",
				matrixSchema, table, matrixLeaves, rows))
		}
	}
	testhelper.AssertQueryRuns(conn, fmt.Sprintf(
		"INSERT INTO %s.mixed_part SELECT i, i %% %d, 'r' || i FROM generate_series(1, %d) i",
		matrixSchema, len(accessMethods), len(accessMethods)*matrixRowsPerLeaf))
}

func dropMatrixSchema(conn *dbconn.DBConn) {
	testhelper.AssertQueryRuns(conn, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", matrixSchema))
}

/*
 * A fingerprint of each table's data: row count, a sum over the key, and a sum
 * of per-row digests. Comparing these across a restore catches rows that are
 * missing, duplicated, or simply wrong, which a row count alone does not.
 *
 * The per-row digests are summed rather than concatenated. An ordered
 * string_agg looks like the obvious thing to use and is unusable here: the
 * segments produce partial aggregates that are concatenated in whatever order
 * they arrive, so it returns a different answer on every execution of the same
 * query against the same data. Addition does not care what order it sees its
 * inputs in.
 */
func getMatrixContents(conn *dbconn.DBConn, tables []string) map[string]string {
	contents := make(map[string]string, len(tables))
	for _, table := range tables {
		contents[table] = dbconn.MustSelectString(conn, fmt.Sprintf(`
			SELECT count(*)::text || ':' || coalesce(sum(id), 0)::text || ':' ||
				coalesce(sum(('x' || substr(md5(id::text || '|' || coalesce(v, '')), 1, 8))::bit(32)::bigint), 0)::text AS string
			FROM %s`, table))
	}
	return contents
}

// Every relation in the matrix schema, root and leaf, mapped to its access
// method. A partitioned root that was never given one reports "".
func getMatrixAccessMethods(conn *dbconn.DBConn) map[string]string {
	var results []struct {
		RelName string
		Am      string
	}
	err := conn.Select(&results, fmt.Sprintf(`
		SELECT quote_ident(n.nspname) || '.' || quote_ident(c.relname) AS relname,
			coalesce(a.amname, '') AS am
		FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_am a ON a.oid = c.relam
		WHERE n.nspname = '%s'
			AND c.relkind IN ('r', 'p')`, matrixSchema))
	Expect(err).ToNot(HaveOccurred())

	accessMethods := make(map[string]string, len(results))
	for _, result := range results {
		accessMethods[result.RelName] = result.Am
	}
	return accessMethods
}

// The tables named by one restore plan entry, keyed by its timestamp.
func getRestorePlan(timestamp string) map[string][]string {
	historyDB, err := history.InitializeHistoryDatabase(historyFilePath)
	Expect(err).ToNot(HaveOccurred())
	backupConfig, err := history.GetBackupConfig(timestamp, historyDB)
	Expect(err).ToNot(HaveOccurred())

	plan := make(map[string][]string, len(backupConfig.RestorePlan))
	for _, entry := range backupConfig.RestorePlan {
		plan[entry.Timestamp] = entry.TableFQNs
	}
	return plan
}

var _ = Describe("End to End storage and partitioning matrix tests", func() {
	var accessMethods []string
	var tables []string

	BeforeEach(func() {
		end_to_end_setup()
		if backupConn.Version.IsGPDB() && backupConn.Version.Before("7") {
			Skip("Matrix uses USING <access method>, which needs GPDB7 or Cloudberry")
		}
		skipIfOldBackupVersionBefore("2.2.0")

		accessMethods = matrixAccessMethods()
		tables = matrixTables(accessMethods)
		createMatrixSchema(backupConn, accessMethods)
		populateMatrixSchema(backupConn, accessMethods)
	})
	AfterEach(func() {
		dropMatrixSchema(backupConn)
		dropMatrixSchema(restoreConn)
		end_to_end_teardown()
	})

	Describe("Full backup and restore", func() {
		It("restores the data of every storage and partitioning combination", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--include-schema", matrixSchema)
			timestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(timestamp)

			gprestore(gprestorePath, restoreHelperPath, timestamp, "--redirect-db", "restoredb")

			Expect(getMatrixContents(restoreConn, tables)).
				To(Equal(getMatrixContents(backupConn, tables)))
		})

		It("restores every leaf under its original access method", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--include-schema", matrixSchema)
			timestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(timestamp)

			gprestore(gprestorePath, restoreHelperPath, timestamp, "--redirect-db", "restoredb")

			backedUp := getMatrixAccessMethods(backupConn)
			restored := getMatrixAccessMethods(restoreConn)

			/*
			 * Known gap, asserted rather than ignored so that closing it is
			 * noticed here: GetTableAccessMethod() in queries_table_defs.go
			 * drops amname = 'heap' because heap is the default for an
			 * ordinary table. The default for a *partitioned* root is no
			 * access method at all, so a root explicitly created USING heap
			 * comes back with none. The leaves are unaffected, and so is the
			 * data; what changes is the access method a later ADD PARTITION
			 * would inherit.
			 */
			heapRoot := fmt.Sprintf("%s.heap_part", matrixSchema)
			Expect(backedUp[heapRoot]).To(Equal("heap"))
			Expect(restored[heapRoot]).To(BeEmpty())
			delete(backedUp, heapRoot)
			delete(restored, heapRoot)

			Expect(restored).To(Equal(backedUp))
		})
	})

	Describe("Incremental backup", func() {
		/*
		 * Change exactly one table per access method, each in a different
		 * shape, and leave every other table alone. The delete is on a PAX
		 * table on purpose: PAX records a delete in the visimap without
		 * adding a micro-partition, so it is the change a marker derived from
		 * the micro-partition sequence would miss.
		 */
		changeOneTablePerAccessMethod := func() []string {
			changed := make([]string, 0, len(accessMethods))

			testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
				"INSERT INTO %s.ao_row_plain SELECT i, i %% %d, 'new' FROM generate_series(1001, 1010) i",
				matrixSchema, matrixLeaves))
			changed = append(changed, fmt.Sprintf("%s.ao_row_plain", matrixSchema))

			// A leaf of a uniform-AM partitioned table: only that leaf moves.
			testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
				"INSERT INTO %s.ao_column_part SELECT i, 1, 'new' FROM generate_series(2001, 2010) i",
				matrixSchema))
			changed = append(changed, fmt.Sprintf("%s.ao_column_part_1_prt_2", matrixSchema))

			for _, am := range accessMethods {
				if am != "pax" {
					continue
				}
				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"DELETE FROM %s.pax_plain WHERE id <= 5", matrixSchema))
				changed = append(changed, fmt.Sprintf("%s.pax_plain", matrixSchema))

				// The pax leaf of the mixed-AM table.
				paxPartition := len(accessMethods) - 1
				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"INSERT INTO %s.mixed_part SELECT i, %d, 'new' FROM generate_series(3001, 3010) i",
					matrixSchema, paxPartition))
				changed = append(changed, fmt.Sprintf("%s.mixed_part_pax", matrixSchema))
			}
			return changed
		}

		// Every heap table in the matrix, root partitions excluded: heap has
		// no change detection, so all of these are re-backed-up every time.
		alwaysBackedUp := func() []string {
			always := []string{
				fmt.Sprintf("%s.heap_plain", matrixSchema),
				fmt.Sprintf("%s.mixed_part_heap", matrixSchema),
			}
			for leaf := 1; leaf <= matrixLeaves; leaf++ {
				always = append(always, fmt.Sprintf("%s.heap_part_1_prt_%d", matrixSchema, leaf))
			}
			return always
		}

		It("re-backs-up heap tables and changed AO and PAX tables, and nothing else", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--include-schema", matrixSchema)
			fullTimestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(fullTimestamp)

			changed := changeOneTablePerAccessMethod()

			output = gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--incremental", "--include-schema", matrixSchema)
			incrementalTimestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(incrementalTimestamp)

			plan := getRestorePlan(incrementalTimestamp)
			Expect(plan).To(HaveLen(2))

			expected := append(alwaysBackedUp(), changed...)
			Expect(plan[incrementalTimestamp]).To(ConsistOf(expected))

			// Everything else stayed where it was, which is the whole point:
			// the unchanged AO and PAX tables are still served by the full
			// backup and were not read again.
			for _, table := range changed {
				Expect(plan[fullTimestamp]).To(Not(ContainElement(table)))
			}
			Expect(plan[fullTimestamp]).To(Not(BeEmpty()))
		})

		It("restores the whole chain when the incremental timestamp is restored normally", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--include-schema", matrixSchema)
			fullTimestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(fullTimestamp)

			changeOneTablePerAccessMethod()

			output = gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--incremental", "--include-schema", matrixSchema)
			incrementalTimestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(incrementalTimestamp)

			gprestore(gprestorePath, restoreHelperPath, incrementalTimestamp,
				"--redirect-db", "restoredb")

			Expect(getMatrixContents(restoreConn, tables)).
				To(Equal(getMatrixContents(backupConn, tables)))
		})

		It("applies the incremental as a delta onto a database restored from the full backup", func() {
			output := gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--include-schema", matrixSchema)
			fullTimestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(fullTimestamp)

			contentsAtFullBackup := getMatrixContents(backupConn, tables)

			gprestore(gprestorePath, restoreHelperPath, fullTimestamp, "--redirect-db", "restoredb")
			Expect(getMatrixContents(restoreConn, tables)).To(Equal(contentsAtFullBackup))

			changeOneTablePerAccessMethod()

			output = gpbackup(gpbackupPath, backupHelperPath,
				"--leaf-partition-data", "--incremental", "--include-schema", matrixSchema)
			incrementalTimestamp := getBackupTimestamp(string(output))
			defer assertArtifactsCleaned(incrementalTimestamp)

			gprestore(gprestorePath, restoreHelperPath, incrementalTimestamp,
				"--redirect-db", "restoredb", "--incremental", "--data-only")

			Expect(getMatrixContents(restoreConn, tables)).
				To(Equal(getMatrixContents(backupConn, tables)))
		})
	})
})
