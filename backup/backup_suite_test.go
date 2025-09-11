package backup_test

/*
 * This file contains integration tests for gpbackup as a whole, rather than
 * tests relating to functions in any particular file.
 */

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-gpbackup/backup"
	"github.com/apache/cloudberry-gpbackup/testutils"
	"github.com/apache/cloudberry-gpbackup/toc"
	"github.com/apache/cloudberry-gpbackup/utils"
	"github.com/spf13/pflag"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var (
	connectionPool *dbconn.DBConn
	defaultConnNum = 0
	mock           sqlmock.Sqlmock
	stdout         *Buffer
	stderr         *Buffer
	logfile        *Buffer
	buffer         *Buffer
	tocfile        *toc.TOC
	backupfile     *utils.FileWithByteCount
)

func TestBackup(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "backup tests")
}

var cmdFlags *pflag.FlagSet

var _ = BeforeEach(func() {
	cmdFlags = pflag.NewFlagSet("gpbackup", pflag.ExitOnError)

	backup.SetCmdFlags(cmdFlags)

	utils.SetPipeThroughProgram(utils.PipeThroughProgram{})

	connectionPool, mock, stdout, stderr, logfile = testutils.SetupTestEnvironment()
	backup.SetConnection(connectionPool)
	backup.InitializeMetadataParams(connectionPool)
	buffer = NewBuffer()
})
