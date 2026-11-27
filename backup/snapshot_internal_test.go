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

package backup

import (
	"errors"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"
	"github.com/jackc/pgx/v5/pgconn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
)

var _ = Describe("snapshot preparation", func() {
	var mock sqlmock.Sqlmock
	var opts *options.Options
	table := Relation{Oid: 10, Schema: "public", Name: "t"}
	BeforeEach(func() {
		connectionPool, mock, _, _, _ = testhelper.SetupTestEnvironment()
		connectionPool.Version = dbconn.NewVersion("2.999.0")
		connectionPool.Version.Type = dbconn.CBDB
		SetCmdFlags(pflag.NewFlagSet("snapshot", pflag.ContinueOnError))
		var err error
		opts, err = options.NewOptions(cmdFlags)
		Expect(err).NotTo(HaveOccurred())
		filterRelationClause = ""
		IncludedRelationFqns = nil
		ExcludedRelationFqns = nil
		heapFileHashes = nil
		backupSnapshot = ""
	})
	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
		connectionPool.Close()
		backupSnapshot = ""
		heapFileHashes = nil
	})

	check := func(missing ...string) {
		rows := sqlmock.NewRows([]string{"fqn"})
		for _, name := range missing {
			rows.AddRow(name)
		}
		mock.ExpectQuery("WITH RECURSIVE.*").WillReturnRows(rows)
	}
	It("accepts tables and descendants already locked before the snapshot", func() {
		check()
		conflicts, err := CheckSnapshotTableLocks(connectionPool, []Relation{table})
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(BeEmpty())
	})
	It("does not query for an empty table set", func() {
		conflicts, err := CheckSnapshotTableLocks(connectionPool, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(BeEmpty())
	})
	It("deduplicates missing descendants across bounded OID batches", func() {
		tables := make([]Relation, 1001)
		for i := range tables {
			tables[i] = Relation{Oid: uint32(i + 10)}
		}
		check("public.leaf", "public.leaf")
		check("public.leaf", "public.other")
		conflicts, err := CheckSnapshotTableLocks(connectionPool, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(Equal([]string{"public.leaf", "public.other"}))
	})
	It("fails closed on a lock validation error", func() {
		mock.ExpectQuery("WITH RECURSIVE").WillReturnError(errors.New("catalog failure"))
		_, err := CheckSnapshotTableLocks(connectionPool, []Relation{table})
		Expect(err).To(MatchError(ContainSubstring("catalog failure")))
	})
	It("bounds conflict diagnostics", func() {
		names := make([]string, 12)
		for i := range names {
			names[i] = fmt.Sprintf("t%d", i)
		}
		summary := snapshotConflictSummary(names)
		Expect(summary).To(ContainSubstring("t9; and 2 more"))
		Expect(summary).NotTo(ContainSubstring("t10"))
	})

	begin := func() {
		mock.ExpectBegin()
		mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL SERIALIZABLE").WillReturnResult(sqlmock.NewResult(0, 0))
	}
	session := func() {
		for _, query := range []string{"SET search_path", "SET statement_timeout", "SET DATESTYLE", "SET enable_mergejoin", "SELECT set_config", "SET synchronize_seqscans", "SET INTERVALSTYLE", "SET lock_timeout", "SET idle_in_transaction_session_timeout"} {
			mock.ExpectExec(query).WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	tables := func() {
		mock.ExpectQuery("SELECT n.oid AS schemaoid").WillReturnRows(sqlmock.NewRows([]string{"schemaoid", "oid", "schema", "name"}).AddRow(1, 10, "public", "t"))
	}
	lock := func(err error) {
		tables()
		begin()
		expectation := mock.ExpectExec("LOCK TABLE public.t")
		if err != nil {
			expectation.WillReturnError(err)
		} else {
			expectation.WillReturnResult(sqlmock.NewResult(0, 0))
		}
	}
	snapshot := func() {
		mock.ExpectQuery("SELECT pg_catalog.pg_export_snapshot").WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow("new-snapshot"))
		tables()
	}
	twoConnections := func() {
		connectionPool.ConnPool[0].SetMaxOpenConns(2)
		connectionPool.ConnPool = append(connectionPool.ConnPool, connectionPool.ConnPool[0])
		connectionPool.Tx = append(connectionPool.Tx, nil)
		connectionPool.NumConns = 2
	}
	It("sets session GUCs before beginning any Cloudberry backup transaction", func() {
		twoConnections()
		for i := 0; i < 2; i++ {
			mock.ExpectExec("SET application_name").WillReturnResult(sqlmock.NewResult(0, 0))
			session()
		}
		Expect(initializeBackupTransactions("20261124000000")).To(Succeed())
		Expect(connectionPool.Tx[0]).To(BeNil())
		Expect(connectionPool.Tx[1]).To(BeNil())
		Expect(backupSnapshot).To(BeEmpty())
	})
	It("locks before exporting and imports workers only after validating the final table set", func() {
		twoConnections()
		lock(nil)
		snapshot()
		check()
		begin()
		mock.ExpectExec("SET TRANSACTION SNAPSHOT 'new-snapshot'").WillReturnResult(sqlmock.NewResult(0, 0))
		Expect(PrepareBackupSnapshot(opts)).To(Succeed())
		Expect(backupSnapshot).To(Equal("new-snapshot"))
	})
	DescribeTable("honors the attempt budget for unprotected tables", func(limit int) {
		Expect(cmdFlags.Set(options.SNAPSHOT_ATTEMPTS, fmt.Sprint(limit))).To(Succeed())
		for i := 0; i < limit; i++ {
			lock(nil)
			snapshot()
			check("public.new_leaf")
			mock.ExpectRollback()
		}
		err := PrepareBackupSnapshot(opts)
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("after %d attempt(s): public.new_leaf", limit))))
		Expect(backupSnapshot).To(BeEmpty())
	}, Entry("one", 1), Entry("three", 3))
	It("keeps heap hashes until they can be verified against the final snapshot", func() {
		heapFileHashes = map[uint32]string{10: "hash"}
		lock(nil)
		snapshot()
		check("public.new_leaf")
		mock.ExpectRollback()
		lock(nil)
		snapshot()
		check()
		Expect(PrepareBackupSnapshot(opts)).To(Succeed())
		Expect(heapFileHashes).To(Equal(map[uint32]string{10: "hash"}))
	})
	DescribeTable("retries missing names", func(code string) {
		lock(&pgconn.PgError{Code: code, Message: "disappeared"})
		mock.ExpectRollback()
		lock(nil)
		snapshot()
		check()
		Expect(PrepareBackupSnapshot(opts)).To(Succeed())
	}, Entry("table", "42P01"), Entry("schema", "3F000"))
	It("does not retry unrelated SQL errors", func() {
		lock(&pgconn.PgError{Code: "42501", Message: "permission denied"})
		Expect(PrepareBackupSnapshot(opts)).To(MatchError(ContainSubstring("permission denied")))
	})
	It("does not import workers after a snapshot export failure", func() {
		twoConnections()
		lock(nil)
		mock.ExpectQuery("SELECT pg_catalog.pg_export_snapshot").WillReturnError(errors.New("export failed"))
		Expect(PrepareBackupSnapshot(opts)).To(MatchError("export failed"))
		Expect(connectionPool.Tx[1]).To(BeNil())
	})
	It("stops on a lock validation query failure", func() {
		lock(nil)
		snapshot()
		mock.ExpectQuery("WITH RECURSIVE").WillReturnError(errors.New("check failed"))
		Expect(PrepareBackupSnapshot(opts)).To(MatchError(ContainSubstring("check failed")))
	})
	It("stops on rollback failure", func() {
		lock(nil)
		snapshot()
		check("public.new_leaf")
		mock.ExpectRollback().WillReturnError(errors.New("rollback failure"))
		Expect(PrepareBackupSnapshot(opts)).To(MatchError(ContainSubstring("rollback failure")))
	})
})
