package restore_test

/*
 * This file contains integration tests for gprestore as a whole, rather than
 * tests relating to functions in any particular file.
 */

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-backup/restore"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var (
	connectionPool *dbconn.DBConn
	mock           sqlmock.Sqlmock
	stdout         *Buffer
	logfile        *Buffer
	buffer         *Buffer
)

func TestRestore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "restore tests")
}

var cmdFlags *pflag.FlagSet

var _ = BeforeEach(func() {
	connectionPool, mock, stdout, _, logfile = testutils.SetupTestEnvironment()
	restore.SetConnection(connectionPool)
	buffer = NewBuffer()

	cmdFlags = pflag.NewFlagSet("gprestore", pflag.ExitOnError)
	restore.SetCmdFlags(cmdFlags)
})
