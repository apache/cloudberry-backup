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
	"database/sql/driver"
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
	columns := []string{"fqn", "oid", "snapshotnode", "currentnode", "currentoid"}
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

	DescribeTable("storage and identity conflicts", func(node, oid driver.Value, oldNode int, reason string) {
		mock.ExpectQuery("WITH RECURSIVE").WillReturnRows(sqlmock.NewRows(columns).AddRow("public.t", 10, oldNode, node, oid))
		conflicts, err := CheckSnapshotTables(connectionPool, []Relation{table})
		Expect(err).NotTo(HaveOccurred())
		if reason == "" {
			Expect(conflicts).To(BeEmpty())
		} else {
			Expect(conflicts).To(ConsistOf(ContainSubstring(reason)))
		}
	},
		Entry("unchanged", 20, 10, 20, ""),
		Entry("rewritten", 21, 10, 20, "storage changed"),
		Entry("missing storage", nil, 10, 20, "storage disappeared"),
		Entry("replaced OID", 20, 11, 20, "snapshot OID"),
		Entry("missing name", 20, nil, 20, "snapshot OID"),
		Entry("no own storage", nil, 10, 0, ""),
	)
	It("does not query for an empty table set", func() {
		conflicts, err := CheckSnapshotTables(connectionPool, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(BeEmpty())
	})
	It("reports nested descendants only once", func() {
		mock.ExpectQuery("WITH RECURSIVE.*JOIN relations").WillReturnRows(sqlmock.NewRows(columns).
			AddRow("public.root", 10, 0, nil, 10).
			AddRow("public.leaf", 11, 20, 21, 11).
			AddRow("public.leaf", 11, 20, 21, 11))
		conflicts, err := CheckSnapshotTables(connectionPool, []Relation{table})
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(HaveLen(1))
		Expect(conflicts[0]).To(ContainSubstring("public.leaf"))
	})
	It("fails closed on a check error", func() {
		mock.ExpectQuery("WITH RECURSIVE").WillReturnError(errors.New("catalog failure"))
		_, err := CheckSnapshotTables(connectionPool, []Relation{table})
		Expect(err).To(MatchError(ContainSubstring("catalog failure")))
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
	restart := func() {
		mock.ExpectRollback()
		mock.ExpectExec("SET application_name").WillReturnResult(sqlmock.NewResult(0, 0))
		begin()
		mock.ExpectQuery("SELECT pg_catalog.pg_export_snapshot").WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow("new-snapshot"))
		session()
	}
	tables := func(lockErr error, changed bool) {
		mock.ExpectQuery("SELECT n.oid AS schemaoid").WillReturnRows(sqlmock.NewRows([]string{"schemaoid", "oid", "schema", "name"}).AddRow(1, 10, "public", "t"))
		lock := mock.ExpectExec("LOCK TABLE public.t")
		if lockErr != nil {
			lock.WillReturnError(lockErr)
			return
		}
		lock.WillReturnResult(sqlmock.NewResult(0, 0))
		node := 20
		if changed {
			node = 21
		}
		mock.ExpectQuery("WITH RECURSIVE").WillReturnRows(sqlmock.NewRows(columns).AddRow("public.t", 10, 20, node, 10))
	}
	It("exports before any worker imports and applies session queries afterwards", func() {
		// Two mock connections share expectations, so ordering verifies import precedes queries.
		connectionPool.ConnPool[0].SetMaxOpenConns(2)
		connectionPool.ConnPool = append(connectionPool.ConnPool, connectionPool.ConnPool[0])
		connectionPool.Tx = append(connectionPool.Tx, nil)
		connectionPool.NumConns = 2
		for i := 0; i < 2; i++ {
			mock.ExpectExec("SET application_name").WillReturnResult(sqlmock.NewResult(0, 0))
			begin()
			if i == 0 {
				mock.ExpectQuery("SELECT pg_catalog.pg_export_snapshot").WillReturnRows(sqlmock.NewRows([]string{"snapshot"}).AddRow("new-snapshot"))
			} else {
				mock.ExpectExec("SET TRANSACTION SNAPSHOT 'new-snapshot'").WillReturnResult(sqlmock.NewResult(0, 0))
			}
			session()
		}
		Expect(initializeBackupTransactions("20261120000000")).To(Succeed())
		Expect(backupSnapshot).To(Equal("new-snapshot"))
	})
	DescribeTable("honors the attempt budget", func(limit int) {
		Expect(cmdFlags.Set(options.SNAPSHOT_ATTEMPTS, fmt.Sprint(limit))).To(Succeed())
		begin()
		connectionPool.MustBegin()
		for i := 1; i <= limit; i++ {
			tables(nil, true)
			if i < limit {
				restart()
			}
		}
		_, err := prepareSnapshotTables(opts)
		Expect(err).To(MatchError(ContainSubstring(fmt.Sprintf("after %d attempt(s): public.t", limit))))
	}, Entry("one", 1), Entry("three", 3))
	It("discards heap hashes and accepts the new snapshot", func() {
		begin()
		connectionPool.MustBegin()
		heapFileHashes = map[uint32]string{10: "old"}
		tables(nil, true)
		restart()
		tables(nil, false)
		result, err := prepareSnapshotTables(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal([]Relation{{SchemaOid: 1, Oid: 10, Schema: "public", Name: "t"}}))
		Expect(heapFileHashes).To(BeNil())
		Expect(backupSnapshot).To(Equal("new-snapshot"))
	})
	It("does not retry unrelated SQL errors", func() {
		tables(&pgconn.PgError{Code: "42501", Message: "permission denied"}, false)
		_, err := prepareSnapshotTables(opts)
		Expect(err).To(MatchError(ContainSubstring("permission denied")))
	})
	DescribeTable("retries missing names", func(code string) {
		begin()
		connectionPool.MustBegin()
		tables(&pgconn.PgError{Code: code, Message: "disappeared"}, false)
		restart()
		tables(nil, false)
		_, err := prepareSnapshotTables(opts)
		Expect(err).NotTo(HaveOccurred())
	}, Entry("table", "42P01"), Entry("schema", "3F000"))
	It("does not validate physical storage for metadata-only backups", func() {
		Expect(cmdFlags.Set(options.METADATA_ONLY, "true")).To(Succeed())
		mock.ExpectQuery("SELECT n.oid AS schemaoid").WillReturnRows(sqlmock.NewRows([]string{"schemaoid", "oid", "schema", "name"}).AddRow(1, 10, "public", "t"))
		mock.ExpectExec("LOCK TABLE public.t").WillReturnResult(sqlmock.NewResult(0, 0))
		result, err := prepareSnapshotTables(opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(HaveLen(1))
	})

	It("does not retry a storage validation query failure", func() {
		mock.ExpectQuery("SELECT n.oid AS schemaoid").WillReturnRows(sqlmock.NewRows([]string{"schemaoid", "oid", "schema", "name"}).AddRow(1, 10, "public", "t"))
		mock.ExpectExec("LOCK TABLE public.t").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("WITH RECURSIVE").WillReturnError(errors.New("check failed"))
		_, err := prepareSnapshotTables(opts)
		Expect(err).To(MatchError(ContainSubstring("check failed")))
	})

	It("stops on rollback failure", func() {
		begin()
		connectionPool.MustBegin()
		tables(nil, true)
		mock.ExpectRollback().WillReturnError(errors.New("rollback failure"))
		_, err := prepareSnapshotTables(opts)
		Expect(err).To(MatchError(ContainSubstring("rollback failure")))
	})
	It("stops on snapshot preparation failure", func() {
		begin()
		connectionPool.MustBegin()
		tables(nil, true)
		mock.ExpectRollback()
		mock.ExpectExec("SET application_name").WillReturnError(errors.New("session failure"))
		_, err := prepareSnapshotTables(opts)
		Expect(err).To(MatchError("session failure"))
	})
})
