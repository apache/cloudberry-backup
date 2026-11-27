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
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("findBlockedQueriesQuery",
	func(dbType dbconn.DBType, version string, expected string) {
		dbVersion := dbconn.NewVersion(version)
		dbVersion.Type = dbType
		Expect(findBlockedQueriesQuery(dbVersion, "20260101010101")).To(Equal(expected))
	},
	Entry("GPDB 5", dbconn.GPDB, "5.29.0", "SELECT procpid from pg_stat_activity WHERE application_name='gpbackup_20260101010101' AND waiting='t' AND waiting_reason='lock';"),
	Entry("GPDB 6", dbconn.GPDB, "6.26.0", "SELECT pid from pg_stat_activity WHERE application_name='gpbackup_20260101010101' AND waiting='t' AND waiting_reason='lock';"),
	Entry("GPDB 7", dbconn.GPDB, "7.1.0", "SELECT pid from pg_stat_activity WHERE application_name='gpbackup_20260101010101' AND wait_event_type='Lock';"),
	Entry("Cloudberry", dbconn.CBDB, "1.6.0", "SELECT pid from pg_stat_activity WHERE application_name='gpbackup_20260101010101' AND wait_event_type='Lock';"),
)

var _ = Describe("cancelBlockedQueriesWithConn", func() {
	const (
		timestamp    = "20260101010101"
		pollInterval = 10 * time.Millisecond
	)
	var (
		conn         *dbconn.DBConn
		mock         sqlmock.Sqlmock
		blockedQuery = regexp.QuoteMeta("SELECT pid from pg_stat_activity WHERE application_name='gpbackup_20260101010101' AND wait_event_type='Lock';")
		cancelQuery  = regexp.QuoteMeta("SELECT pg_cancel_backend(101)")
		cancelQuery2 = regexp.QuoteMeta("SELECT pg_cancel_backend(102)")
	)
	BeforeEach(func() {
		testhelper.SetupTestLogger()
		conn, mock = testhelper.CreateAndConnectMockDB(1)
		testhelper.SetDBVersion(conn, "1.6.0")
		conn.Version.Type = dbconn.CBDB
	})
	AfterEach(func() {
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	It("does nothing when no query is blocked", func() {
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}))
		Expect(cancelBlockedQueriesWithConn(context.Background(), conn, timestamp, pollInterval)).To(Succeed())
	})
	It("cancels blocked queries and waits until none are blocked", func() {
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101))
		mock.ExpectExec(cancelQuery).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}))
		Expect(cancelBlockedQueriesWithConn(context.Background(), conn, timestamp, pollInterval)).To(Succeed())
	})
	It("still cancels the remaining queries when one cancel fails", func() {
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101).AddRow(102))
		mock.ExpectExec(cancelQuery).WillReturnError(errors.New("cancel failed"))
		mock.ExpectExec(cancelQuery2).WillReturnResult(sqlmock.NewResult(0, 1))
		Expect(cancelBlockedQueriesWithConn(context.Background(), conn, timestamp, pollInterval)).To(MatchError("could not cancel 1 of 2 blocked queries"))
	})
	It("returns once the deadline passes while a query cannot run", func() {
		mock.ExpectQuery(blockedQuery).WillDelayFor(time.Minute).WillReturnRows(sqlmock.NewRows([]string{"pid"}))
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		Expect(cancelBlockedQueriesWithConn(ctx, conn, timestamp, pollInterval)).NotTo(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
	})
	It("returns once the deadline passes while waiting for canceled queries", func() {
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101))
		mock.ExpectExec(cancelQuery).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(blockedQuery).WillDelayFor(time.Minute).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101))
		// The deadline leaves ample time for the first poll to start, so the
		// last query is always reached.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		start := time.Now()
		Expect(cancelBlockedQueriesWithConn(ctx, conn, timestamp, pollInterval)).NotTo(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
	})
})
