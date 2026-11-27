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

package utils

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

var _ = Describe("RunWithContext", func() {
	It("returns the function's result", func() {
		Expect(RunWithContext(context.Background(), func() error { return nil })).To(Succeed())
		Expect(RunWithContext(context.Background(), func() error { return errors.New("boom") })).To(MatchError("boom"))
	})
	It("returns a panic as an error", func() {
		Expect(RunWithContext(context.Background(), func() error { panic("boom") })).To(MatchError("boom"))
	})
	It("stops waiting at the deadline for a function that does not return", func() {
		release := make(chan struct{})
		defer close(release)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		Expect(RunWithContext(ctx, func() error { <-release; return nil })).To(MatchError(context.DeadlineExceeded))
	})
})

var _ = Describe("terminateHangingCopySessionsWithConn", func() {
	const (
		appName      = "gpbackup_20260101010101"
		copyFileName = "/tmp/gpbackup_pipe"
		interval     = 50 * time.Millisecond
	)
	var (
		conn       *dbconn.DBConn
		mock       sqlmock.Sqlmock
		countQuery = regexp.QuoteMeta("count(*) FROM pg_stat_activity")
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

	It("returns once no COPY session is left", func() {
		mock.ExpectQuery(countQuery).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		Expect(terminateHangingCopySessionsWithConn(context.Background(), conn, copyFileName, appName, interval)).To(Succeed())
	})
	It("returns a query error", func() {
		mock.ExpectQuery(countQuery).WillReturnError(errors.New("count failed"))
		Expect(terminateHangingCopySessionsWithConn(context.Background(), conn, copyFileName, appName, interval)).To(MatchError("count failed"))
	})
	It("returns once the deadline passes while a query cannot run", func() {
		mock.ExpectQuery(countQuery).WillDelayFor(time.Minute).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		start := time.Now()
		Expect(terminateHangingCopySessionsWithConn(ctx, conn, copyFileName, appName, interval)).NotTo(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
	})
})

var _ = Describe("terminateHangingCopySessionsWithConn with sessions that never end", func() {
	It("keeps terminating them until the deadline passes", func() {
		testhelper.SetupTestLogger()
		conn, mock := testhelper.CreateAndConnectMockDB(1)
		testhelper.SetDBVersion(conn, "1.6.0")
		conn.Version.Type = dbconn.CBDB
		mock.MatchExpectationsInOrder(false)
		// More expectations than the loop can use before the deadline, so not
		// all of them are met.
		for i := 0; i < 200; i++ {
			mock.ExpectQuery(regexp.QuoteMeta("count(*) FROM pg_stat_activity")).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
			mock.ExpectExec(regexp.QuoteMeta("pg_terminate_backend(pid) FROM pg_stat_activity")).WillReturnResult(sqlmock.NewResult(0, 1))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		// The deadline can end the loop or a running query, so the error can be
		// either; the caller checks ctx to tell a timeout apart.
		Expect(terminateHangingCopySessionsWithConn(ctx, conn, "/tmp/gpbackup_pipe", "gpbackup_20260101010101", 50*time.Millisecond)).NotTo(Succeed())
		Expect(ctx.Err()).To(MatchError(context.DeadlineExceeded))
		Expect(time.Since(start)).To(BeNumerically("<", 10*time.Second))
	})
})
