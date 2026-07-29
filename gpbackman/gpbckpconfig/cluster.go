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
	"fmt"
	"strconv"

	"github.com/apache/cloudberry-backup/gpbackman/textmsg"
	"github.com/apache/cloudberry-go-libs/operating"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

type SegmentConfig struct {
	ContentID string
	Hostname  string
	DataDir   string
}

// StandbyCoordinator stores the standby coordinator connection target.
type StandbyCoordinator struct {
	Hostname string `db:"hostname"`
	DataDir  string `db:"datadir"`
}

const (
	defaultClusterDatabase       = "postgres"
	primaryCoordinatorDataDirSQL = "SELECT datadir FROM gp_segment_configuration WHERE content = -1 AND role = 'p' AND status = 'u';"
	upStandbyCoordinatorSQL      = "SELECT hostname, datadir FROM gp_segment_configuration WHERE content = -1 AND role = 'm' AND status = 'u';"
)

var connectLocalCluster = sqlx.Connect

// NewClusterLocalClusterConn creates a new connection to the local postgres database.
// Returns an error if the connection could not be established.
func NewClusterLocalClusterConn(dbName string) (*sqlx.DB, error) {
	if dbName == "" {
		return nil, textmsg.ErrorEmptyDatabase()
	}
	username := operating.System.Getenv("PGUSER")
	if username == "" {
		currentUser, _ := operating.System.CurrentUser()
		username = currentUser.Username
	}
	host := operating.System.Getenv("PGHOST")
	if host == "" {
		host, _ = operating.System.Hostname()
	}
	port, err := strconv.Atoi(operating.System.Getenv("PGPORT"))
	if err != nil {
		port = 5432
	}
	connStr := fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable&connect_timeout=60", username, host, port, dbName)
	return connectLocalCluster("postgres", connStr)
}

// NewClusterLocalClusterDefaultConn creates a local cluster connection using PGDATABASE or postgres.
func NewClusterLocalClusterDefaultConn() (*sqlx.DB, error) {
	dbName := operating.System.Getenv("PGDATABASE")
	if dbName == "" {
		dbName = defaultClusterDatabase
	}
	return NewClusterLocalClusterConn(dbName)
}

// GetPrimaryCoordinatorDataDir returns the up primary coordinator data directory.
func GetPrimaryCoordinatorDataDir() (string, error) {
	db, err := NewClusterLocalClusterDefaultConn()
	if err != nil {
		return "", err
	}
	defer func() {
		_ = db.Close()
	}()
	return QueryPrimaryCoordinatorDataDir(db)
}

// QueryPrimaryCoordinatorDataDir queries the up primary coordinator data directory.
func QueryPrimaryCoordinatorDataDir(conn *sqlx.DB) (string, error) {
	return ExecuteQueryLocalClusterConn[string](conn, primaryCoordinatorDataDirSQL)
}

// GetUpStandbyCoordinator returns the up standby coordinator from the local cluster catalog.
func GetUpStandbyCoordinator() (StandbyCoordinator, error) {
	db, err := NewClusterLocalClusterDefaultConn()
	if err != nil {
		return StandbyCoordinator{}, err
	}
	defer func() {
		_ = db.Close()
	}()
	return QueryUpStandbyCoordinator(db)
}

// QueryUpStandbyCoordinator queries the up standby coordinator from the local cluster catalog.
func QueryUpStandbyCoordinator(conn *sqlx.DB) (StandbyCoordinator, error) {
	return ExecuteQueryLocalClusterConn[StandbyCoordinator](conn, upStandbyCoordinatorSQL)
}

// ExecuteQueryLocalClusterConn executes a query on the local cluster connection and returns the result.
// The function is generic and can handle different types of results based on the type parameter T.
//
// Parameters:
//   - conn: A pointer to the sqlx.DB connection object.
//   - query: A string containing the SQL query to be executed.
//
// Returns:
//   - T: The result of the query, which can be of any type specified by the caller.
//   - error: An error object if the query execution fails or if the type is unsupported.
//
// The function supports the following types for T:
//   - string: The result will be a single string value.
//   - []SegmentConfig: The result will be a slice of SegmentConfig structs.
//   - StandbyCoordinator: The result will be a standby coordinator struct.
//
// If the type T is not supported, the function returns an error indicating the unsupported type.
func ExecuteQueryLocalClusterConn[T any](conn *sqlx.DB, query string) (T, error) {
	var result T
	switch any(result).(type) {
	case string:
		var data string
		err := conn.Get(&data, query)
		if err != nil {
			return result, err
		}
		result = any(data).(T)
	case []SegmentConfig:
		var segConfigs []SegmentConfig
		err := conn.Select(&segConfigs, query)
		if err != nil {
			return result, err
		}
		result = any(segConfigs).(T)
	case StandbyCoordinator:
		var standbyCoordinator StandbyCoordinator
		err := conn.Get(&standbyCoordinator, query)
		if err != nil {
			return result, err
		}
		result = any(standbyCoordinator).(T)
	default:
		return result, fmt.Errorf("unsupported type")
	}
	return result, nil
}
