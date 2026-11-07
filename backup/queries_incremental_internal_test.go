package backup

import (
	"errors"
	"regexp"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup/queries_incremental heap file hash internals", func() {
	var (
		conn            *dbconn.DBConn
		mock            sqlmock.Sqlmock
		savedFilter     string
		columnTypeQuery = regexp.QuoteMeta("WITH RECURSIVE type_deps")
	)
	BeforeEach(func() {
		testhelper.SetupTestLogger()
		conn, mock = testhelper.CreateAndConnectMockDB(1)
		testhelper.SetDBVersion(conn, "1.6.0")
		conn.Version.Type = dbconn.CBDB
		savedFilter = filterRelationClause
		filterRelationClause = "n.nspname NOT LIKE 'pg_temp_%'"
	})
	AfterEach(func() {
		filterRelationClause = savedFilter
		Expect(mock.ExpectationsWereMet()).To(Succeed())
	})

	Describe("keepUnchangedHashes", func() {
		It("keeps only hashes that were re-read unchanged", func() {
			tables := []heapTable{{Oid: 1, FQN: "public.same"}, {Oid: 2, FQN: "public.changed"}, {Oid: 3, FQN: "public.unreadable"}}
			hashes := map[uint32]string{1: "h1", 2: "h2", 3: "h3"}
			current := map[uint32]string{1: "h1", 2: "h2b"}
			Expect(keepUnchangedHashes(tables, hashes, current)).To(Equal(map[uint32]string{1: "h1"}))
		})
	})

	Describe("getTablesWithActiveWriters", func() {
		It("returns the oids holding write locks", func() {
			mock.ExpectQuery(regexp.QuoteMeta("FROM pg_catalog.pg_locks")).
				WillReturnRows(sqlmock.NewRows([]string{"oid"}).AddRow(16384).AddRow(16390))
			oids, err := getTablesWithActiveWriters(conn)
			Expect(err).ToNot(HaveOccurred())
			Expect(oids).To(Equal(map[uint32]bool{16384: true, 16390: true}))
		})
		It("returns the query error", func() {
			mock.ExpectQuery(regexp.QuoteMeta("FROM pg_catalog.pg_locks")).WillReturnError(errors.New("boom"))
			_, err := getTablesWithActiveWriters(conn)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("getHeapColumnHashes", func() {
		It("hashes the table's own columns, follows multirange types and excludes unlogged tables on Cloudberry", func() {
			mock.ExpectQuery(columnTypeQuery + "(?s).*rngmultitypid.*relpersistence = 'p'.*FROM pg_attribute ca").
				WillReturnRows(sqlmock.NewRows([]string{"oid", "columnsmd5"}).AddRow(16384, "abc"))
			hashes, err := getHeapColumnHashes(conn)
			Expect(err).ToNot(HaveOccurred())
			Expect(hashes).To(Equal(map[uint32]string{16384: "abc"}))
		})
		It("rolls back to a savepoint when the query fails inside the backup transaction", func() {
			mock.ExpectBegin()
			mock.ExpectExec("SET TRANSACTION ISOLATION LEVEL").WillReturnResult(sqlmock.NewResult(0, 0))
			conn.MustBegin(0)
			mock.ExpectExec(regexp.QuoteMeta("SAVEPOINT gpbackup_heap_columns")).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery(columnTypeQuery).WillReturnError(errors.New("boom"))
			mock.ExpectExec(regexp.QuoteMeta("ROLLBACK TO SAVEPOINT gpbackup_heap_columns")).WillReturnResult(sqlmock.NewResult(0, 0))
			_, err := getHeapColumnHashes(conn)
			Expect(err).To(HaveOccurred())
		})
		It("does not query GPDB 5", func() {
			testhelper.SetDBVersion(conn, "5.1.0")
			conn.Version.Type = dbconn.GPDB
			hashes, err := getHeapColumnHashes(conn)
			Expect(err).ToNot(HaveOccurred())
			Expect(hashes).To(BeEmpty())
		})
	})

	Describe("getHeapIncrementalEntries", func() {
		It("combines DDL timestamps and column hashes by oid", func() {
			mock.ExpectQuery(regexp.QuoteMeta("FROM pg_stat_last_operation")).
				WillReturnRows(sqlmock.NewRows([]string{"oid", "tablefqn", "lastddltimestamp"}).AddRow(1, "public.t", "ts1").AddRow(2, "public.u", ""))
			mock.ExpectQuery(columnTypeQuery).
				WillReturnRows(sqlmock.NewRows([]string{"oid", "columnsmd5"}).AddRow(1, "abc"))
			Expect(getHeapIncrementalEntries(conn)).To(Equal(map[uint32]heapIncrementalEntry{
				1: {FQN: "public.t", Entry: toc.HeapEntry{LastDDLTimestamp: "ts1", ColumnsMD5: "abc"}},
				2: {FQN: "public.u", Entry: toc.HeapEntry{LastDDLTimestamp: ""}},
			}))
		})
		It("returns nil when the columns cannot be read", func() {
			mock.ExpectQuery(regexp.QuoteMeta("FROM pg_stat_last_operation")).
				WillReturnRows(sqlmock.NewRows([]string{"oid", "tablefqn", "lastddltimestamp"}).AddRow(1, "public.t", "ts1"))
			mock.ExpectQuery(columnTypeQuery).WillReturnError(errors.New("boom"))
			Expect(getHeapIncrementalEntries(conn)).To(BeNil())
		})
	})
})
