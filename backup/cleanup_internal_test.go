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

var _ = Describe("runWithContext", func() {
	It("returns the function's result", func() {
		Expect(runWithContext(context.Background(), func() error { return nil })).To(Succeed())
		Expect(runWithContext(context.Background(), func() error { return errors.New("boom") })).To(MatchError("boom"))
	})
	It("returns a panic as an error", func() {
		Expect(runWithContext(context.Background(), func() error { panic("boom") })).To(MatchError("boom"))
	})
	It("stops waiting at the deadline for a function that does not return", func() {
		release := make(chan struct{})
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		Expect(runWithContext(ctx, func() error { <-release; return nil })).To(MatchError(context.DeadlineExceeded))
	})
})

var _ = Describe("cancelBlockedQueriesWithConn", func() {
	const timestamp = "20260101010101"
	var (
		conn         *dbconn.DBConn
		mock         sqlmock.Sqlmock
		blockedQuery = regexp.QuoteMeta("SELECT pid from pg_stat_activity WHERE application_name='gpbackup_20260101010101' AND wait_event_type='Lock';")
		cancelQuery  = regexp.QuoteMeta("SELECT pg_cancel_backend(101)")
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
		Expect(cancelBlockedQueriesWithConn(context.Background(), conn, timestamp)).To(Succeed())
	})
	It("cancels blocked queries and waits until none are blocked", func() {
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101))
		mock.ExpectExec(cancelQuery).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}))
		Expect(cancelBlockedQueriesWithConn(context.Background(), conn, timestamp)).To(Succeed())
	})
	It("returns once the deadline passes while a query cannot run", func() {
		mock.ExpectQuery(blockedQuery).WillDelayFor(time.Minute).WillReturnRows(sqlmock.NewRows([]string{"pid"}))
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		start := time.Now()
		Expect(cancelBlockedQueriesWithConn(ctx, conn, timestamp)).NotTo(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
	})
	It("returns once the deadline passes while waiting for canceled queries", func() {
		mock.ExpectQuery(blockedQuery).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101))
		mock.ExpectExec(cancelQuery).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(blockedQuery).WillDelayFor(time.Minute).WillReturnRows(sqlmock.NewRows([]string{"pid"}).AddRow(101))
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		start := time.Now()
		Expect(cancelBlockedQueriesWithConn(ctx, conn, timestamp)).NotTo(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
	})
})
