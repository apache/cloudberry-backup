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

package backup_test

import (
	"database/sql/driver"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup/queries_incremental tests", func() {
	Describe("GetPAXIncrementalMetadata", func() {
		BeforeEach(func() {
			// SetDBVersion resets the connection's DB type, so set the type
			// afterwards to model a Cloudberry cluster.
			testhelper.SetDBVersion(connectionPool, "2.0.0")
			connectionPool.Version.Type = dbconn.CBDB
		})

		It("issues no queries and returns no entries on GPDB", func() {
			testhelper.SetDBVersion(connectionPool, "7.0.0")
			connectionPool.Version.Type = dbconn.GPDB

			Expect(backup.GetPAXIncrementalMetadata(connectionPool)).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("returns no entries when the database has no PAX tables", func() {
			noAuxRelations := sqlmock.NewRows([]string{"paxtablefqn", "auxtablefqn", "auxkind"})
			mock.ExpectQuery(`pg_ext_aux`).WillReturnRows(noAuxRelations)

			Expect(backup.GetPAXIncrementalMetadata(connectionPool)).To(BeEmpty())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("builds an entry for each PAX table in either aux layout", func() {
			auxRelations := sqlmock.NewRows([]string{"paxtablefqn", "auxtablefqn", "auxkind"}).
				AddRow([]driver.Value{"public.pax_manifest", "pg_ext_aux.pg_manifest_16384", "manifest"}...).
				AddRow([]driver.Value{"public.pax_blocks", "pg_ext_aux.pg_pax_blocks_16385", "blocks"}...)
			mock.ExpectQuery(`pg_ext_aux`).WillReturnRows(auxRelations)

			// One marker query per aux relation, in the order they were returned.
			mock.ExpectQuery(`gp_dist_random\('pg_ext_aux.pg_manifest_16384'\)`).
				WillReturnRows(sqlmock.NewRows([]string{"marker"}).AddRow("manifestdigest"))
			mock.ExpectQuery(`gp_dist_random\('pg_ext_aux.pg_pax_blocks_16385'\)`).
				WillReturnRows(sqlmock.NewRows([]string{"marker"}).AddRow("blocksdigest"))

			lastDDLTimestamps := sqlmock.NewRows([]string{"aotablefqn", "lastddltimestamp"}).
				AddRow([]driver.Value{"public.pax_manifest", "2026-01-01 00:00:00"}...).
				AddRow([]driver.Value{"public.pax_blocks", "2026-01-02 00:00:00"}...)
			mock.ExpectQuery(`pg_stat_last_operation`).WillReturnRows(lastDDLTimestamps)

			result := backup.GetPAXIncrementalMetadata(connectionPool)

			Expect(result).To(HaveLen(2))
			Expect(result["public.pax_manifest"]).To(Equal(toc.PAXEntry{
				Marker: "manifestdigest", LastDDLTimestamp: "2026-01-01 00:00:00"}))
			Expect(result["public.pax_blocks"]).To(Equal(toc.PAXEntry{
				Marker: "blocksdigest", LastDDLTimestamp: "2026-01-02 00:00:00"}))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("records an empty marker for a PAX table whose aux relation is empty", func() {
			auxRelations := sqlmock.NewRows([]string{"paxtablefqn", "auxtablefqn", "auxkind"}).
				AddRow([]driver.Value{"public.pax_empty", "pg_ext_aux.pg_manifest_16386", "manifest"}...)
			mock.ExpectQuery(`pg_ext_aux`).WillReturnRows(auxRelations)
			mock.ExpectQuery(`gp_dist_random`).
				WillReturnRows(sqlmock.NewRows([]string{"marker"}).AddRow(""))
			mock.ExpectQuery(`pg_stat_last_operation`).
				WillReturnRows(sqlmock.NewRows([]string{"aotablefqn", "lastddltimestamp"}).
					AddRow([]driver.Value{"public.pax_empty", "2026-01-03 00:00:00"}...))

			result := backup.GetPAXIncrementalMetadata(connectionPool)

			Expect(result).To(HaveLen(1))
			Expect(result["public.pax_empty"].Marker).To(BeEmpty())
			Expect(result["public.pax_empty"].LastDDLTimestamp).To(Equal("2026-01-03 00:00:00"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})
})
