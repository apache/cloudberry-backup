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
	"strings"

	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

/*
 * A rewrite replaces a table's storage without going through DML. gpbackup
 * still has to notice that the table's contents moved on, because an
 * incremental backup that skips the table leaves the restore plan pointing at
 * a copy taken before the rewrite.
 *
 * CLUSTER is the interesting rewrite. Every other one is spelled ALTER and is
 * therefore recorded in pg_stat_last_operation, which the change detector
 * reads; CLUSTER is not. That leaves the storage-level signal as the only
 * thing standing between a clustered table and a silently stale backup.
 *
 * heap is not covered here on purpose: it has no change detection at all and
 * is re-read on every incremental, so a rewrite can never be missed for it.
 */

const rewriteSchema = "rewrite"

var rewriteCases = []struct {
	accessMethod string
	// What SET ACCESS METHOD should convert this table to, moving it onto a
	// fresh pg_aoseg. Empty where the conversion is not supported: PAX
	// rejects it outright with "PAX not allow swap relation files for
	// different AM", so there is no such rewrite to detect.
	otherAccessMethod string
	requiresPAX       bool
}{
	{accessMethod: "ao_row", otherAccessMethod: "ao_column"},
	{accessMethod: "ao_column", otherAccessMethod: "ao_row"},
	{accessMethod: "pax", requiresPAX: true},
}

/*
 * The rewrites that reset the change signal without being recorded in
 * pg_stat_last_operation.  Every other rewrite is spelled ALTER in a form the
 * catalog does record, and is caught by the DDL timestamp; these three are
 * caught only by the table's storage identity.
 */
var rewriteStatements = []struct {
	label     string
	statement string // takes the table FQN, then the other access method
}{
	{label: "CLUSTER", statement: "CLUSTER %s USING t_idx"},
	{label: "SET UNLOGGED", statement: "ALTER TABLE %s SET UNLOGGED"},
	{label: "SET ACCESS METHOD", statement: "ALTER TABLE %s SET ACCESS METHOD %s"},
}

var _ = Describe("End to End storage rewrite detection tests", func() {
	BeforeEach(func() {
		end_to_end_setup()
		if backupConn.Version.IsGPDB() && backupConn.Version.Before("7") {
			Skip("Test uses USING <access method>, which needs GPDB7 or Cloudberry")
		}
		skipIfOldBackupVersionBefore("2.2.0")
	})
	AfterEach(func() {
		testhelper.AssertQueryRuns(backupConn,
			fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", rewriteSchema))
		testhelper.AssertQueryRuns(restoreConn,
			fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", rewriteSchema))
		end_to_end_teardown()
	})

	for _, rewriteCase := range rewriteCases {
		for _, rewrite := range rewriteStatements {
			rewriteCase, rewrite := rewriteCase, rewrite

			It(fmt.Sprintf("re-backs-up a %s table that %s rewrote",
				rewriteCase.accessMethod, rewrite.label), func() {
				if rewriteCase.requiresPAX {
					skipIfNoPAX()
				}
				if strings.Contains(rewrite.statement, "ACCESS METHOD") &&
					rewriteCase.otherAccessMethod == "" {
					Skip(fmt.Sprintf("%s cannot be converted to another access method",
						rewriteCase.accessMethod))
				}
				tableFQN := fmt.Sprintf("%s.t", rewriteSchema)

				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"DROP SCHEMA IF EXISTS %s CASCADE; CREATE SCHEMA %s", rewriteSchema, rewriteSchema))
				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"CREATE TABLE %s (id int, v text) USING %s DISTRIBUTED BY (id)",
					tableFQN, rewriteCase.accessMethod))
				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"INSERT INTO %s SELECT i, 'v' || i FROM generate_series(1, 100000) i", tableFQN))
				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"CREATE INDEX t_idx ON %s (id)", tableFQN))

				/*
				 * Back up while the table has been written by exactly one
				 * statement. That is the ordinary state of a bulk-loaded table,
				 * and it is also the state a rewrite returns the AO modcount to,
				 * which is what makes the two indistinguishable.
				 */
				output := gpbackup(gpbackupPath, backupHelperPath,
					"--leaf-partition-data", "--include-schema", rewriteSchema)
				fullTimestamp := getBackupTimestamp(string(output))
				defer assertArtifactsCleaned(fullTimestamp)

				testhelper.AssertQueryRuns(backupConn, fmt.Sprintf(
					"DELETE FROM %s WHERE id <= 30000", tableFQN))

				rewriteStatement := rewrite.statement
				if strings.Contains(rewriteStatement, "ACCESS METHOD") {
					rewriteStatement = fmt.Sprintf(rewriteStatement, tableFQN, rewriteCase.otherAccessMethod)
				} else {
					rewriteStatement = fmt.Sprintf(rewriteStatement, tableFQN)
				}
				testhelper.AssertQueryRuns(backupConn, rewriteStatement)

				rowsInSource := dbconn.MustSelectString(backupConn,
					fmt.Sprintf("SELECT count(*) AS string FROM %s", tableFQN))
				Expect(rowsInSource).To(Equal("70000"), "the delete should have taken effect in the source")

				output = gpbackup(gpbackupPath, backupHelperPath,
					"--leaf-partition-data", "--incremental", "--include-schema", rewriteSchema)
				incrementalTimestamp := getBackupTimestamp(string(output))
				defer assertArtifactsCleaned(incrementalTimestamp)

				// The proximate symptom: the table is left behind in the full
				// backup's entry instead of moving to the incremental's.
				plan := getRestorePlan(incrementalTimestamp)
				Expect(plan[incrementalTimestamp]).To(ContainElement(tableFQN),
					"a rewritten table must be re-read by the next incremental backup")

				// The actual harm: what comes back out.
				gprestore(gprestorePath, restoreHelperPath, incrementalTimestamp,
					"--redirect-db", "restoredb")
				assertDataRestored(restoreConn, map[string]int{tableFQN: 70000})
			})
		}
	}
})
