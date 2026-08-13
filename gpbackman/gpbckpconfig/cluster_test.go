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

package gpbckpconfig

import (
	"database/sql"
	"errors"
	"os"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type savedEnvValue struct {
	value string
	ok    bool
}

var _ = Describe("cluster tests", func() {
	var (
		originalConnect func(string, string) (*sqlx.DB, error)
		savedEnv        map[string]savedEnvValue
	)

	BeforeEach(func() {
		originalConnect = connectLocalCluster
		savedEnv = saveClusterEnv("PGDATABASE", "PGUSER", "PGHOST", "PGPORT")
	})

	AfterEach(func() {
		connectLocalCluster = originalConnect
		restoreClusterEnv(savedEnv)
	})

	Describe("NewClusterLocalClusterDefaultConn", func() {
		It("uses PGDATABASE when it is set", func() {
			setClusterEnv(map[string]string{
				"PGDATABASE": "template1",
				"PGUSER":     "backup_user",
				"PGHOST":     "coordinator",
				"PGPORT":     "15432",
			})
			sqlDB, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer sqlDB.Close()
			var gotDriver string
			var gotConnStr string
			connectLocalCluster = func(driverName, dataSourceName string) (*sqlx.DB, error) {
				gotDriver = driverName
				gotConnStr = dataSourceName
				return sqlx.NewDb(sqlDB, "sqlmock"), nil
			}

			db, err := NewClusterLocalClusterDefaultConn()

			Expect(err).NotTo(HaveOccurred())
			mock.ExpectClose()
			Expect(db.Close()).To(Succeed())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
			Expect(gotDriver).To(Equal("postgres"))
			Expect(gotConnStr).To(Equal("postgres://backup_user@coordinator:15432/template1?sslmode=disable&connect_timeout=60"))
		})

		It("falls back to postgres when PGDATABASE is not set", func() {
			setClusterEnv(map[string]string{
				"PGUSER": "backup_user",
				"PGHOST": "coordinator",
				"PGPORT": "15432",
			})
			sqlDB, mock, err := sqlmock.New()
			Expect(err).NotTo(HaveOccurred())
			defer sqlDB.Close()
			var gotConnStr string
			connectLocalCluster = func(driverName, dataSourceName string) (*sqlx.DB, error) {
				gotConnStr = dataSourceName
				return sqlx.NewDb(sqlDB, "sqlmock"), nil
			}

			db, err := NewClusterLocalClusterDefaultConn()

			Expect(err).NotTo(HaveOccurred())
			mock.ExpectClose()
			Expect(db.Close()).To(Succeed())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
			Expect(gotConnStr).To(Equal("postgres://backup_user@coordinator:15432/postgres?sslmode=disable&connect_timeout=60"))
		})

		It("returns connection errors", func() {
			setClusterEnv(map[string]string{
				"PGUSER": "backup_user",
				"PGHOST": "coordinator",
				"PGPORT": "15432",
			})
			connectErr := errors.New("connection failed")
			connectLocalCluster = func(driverName, dataSourceName string) (*sqlx.DB, error) {
				return nil, connectErr
			}

			db, err := NewClusterLocalClusterDefaultConn()

			Expect(db).To(BeNil())
			Expect(err).To(MatchError(connectErr))
		})
	})

	Describe("QueryPrimaryCoordinatorDataDir", func() {
		It("returns the primary coordinator data directory", func() {
			db, mock := newClusterSQLMock()
			defer db.Close()
			mock.ExpectQuery(regexp.QuoteMeta(primaryCoordinatorDataDirSQL)).
				WillReturnRows(sqlmock.NewRows([]string{"datadir"}).AddRow("/data/primary"))

			dataDir, err := QueryPrimaryCoordinatorDataDir(db)

			Expect(err).NotTo(HaveOccurred())
			Expect(dataDir).To(Equal("/data/primary"))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("returns query errors", func() {
			db, mock := newClusterSQLMock()
			defer db.Close()
			queryErr := errors.New("query failed")
			mock.ExpectQuery(regexp.QuoteMeta(primaryCoordinatorDataDirSQL)).WillReturnError(queryErr)

			dataDir, err := QueryPrimaryCoordinatorDataDir(db)

			Expect(dataDir).To(BeEmpty())
			Expect(err).To(MatchError(queryErr))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	Describe("QueryUpStandbyCoordinator", func() {
		It("returns the up standby coordinator", func() {
			db, mock := newClusterSQLMock()
			defer db.Close()
			mock.ExpectQuery(regexp.QuoteMeta(upStandbyCoordinatorSQL)).
				WillReturnRows(sqlmock.NewRows([]string{"hostname", "datadir"}).AddRow("standby-host", "/data/standby"))

			standbyCoordinator, err := QueryUpStandbyCoordinator(db)

			Expect(err).NotTo(HaveOccurred())
			Expect(standbyCoordinator).To(Equal(StandbyCoordinator{
				Hostname: "standby-host",
				DataDir:  "/data/standby",
			}))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("returns sql.ErrNoRows when no up standby is present", func() {
			db, mock := newClusterSQLMock()
			defer db.Close()
			mock.ExpectQuery(regexp.QuoteMeta(upStandbyCoordinatorSQL)).
				WillReturnRows(sqlmock.NewRows([]string{"hostname", "datadir"}))

			standbyCoordinator, err := QueryUpStandbyCoordinator(db)

			Expect(standbyCoordinator).To(Equal(StandbyCoordinator{}))
			Expect(errors.Is(err, sql.ErrNoRows)).To(BeTrue())
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})

		It("returns query errors", func() {
			db, mock := newClusterSQLMock()
			defer db.Close()
			queryErr := errors.New("query failed")
			mock.ExpectQuery(regexp.QuoteMeta(upStandbyCoordinatorSQL)).WillReturnError(queryErr)

			standbyCoordinator, err := QueryUpStandbyCoordinator(db)

			Expect(standbyCoordinator).To(Equal(StandbyCoordinator{}))
			Expect(err).To(MatchError(queryErr))
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

})

func newClusterSQLMock() (*sqlx.DB, sqlmock.Sqlmock) {
	sqlDB, mock, err := sqlmock.New()
	Expect(err).NotTo(HaveOccurred())
	return sqlx.NewDb(sqlDB, "sqlmock"), mock
}

func saveClusterEnv(names ...string) map[string]savedEnvValue {
	saved := make(map[string]savedEnvValue, len(names))
	for _, name := range names {
		value, ok := os.LookupEnv(name)
		saved[name] = savedEnvValue{value: value, ok: ok}
		_ = os.Unsetenv(name)
	}
	return saved
}

func restoreClusterEnv(saved map[string]savedEnvValue) {
	for name, envValue := range saved {
		if envValue.ok {
			_ = os.Setenv(name, envValue.value)
			continue
		}
		_ = os.Unsetenv(name)
	}
}

func setClusterEnv(values map[string]string) {
	for name, value := range values {
		_ = os.Setenv(name, value)
	}
}
