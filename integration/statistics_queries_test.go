package integration

import (
	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-go-libs/structmatcher"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup integration tests", func() {
	tables := []backup.Table{
		{Relation: backup.Relation{Schema: "public", Name: "foo"}},
	}
	var tableOid uint32
	BeforeEach(func() {
		testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.foo(i int, j text, k bool)")
		tableOid = testutils.OidFromObjectName(connectionPool, "public", "foo", backup.TYPE_RELATION)
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (1, 'a', 't')")
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (2, 'b', 'f')")
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (3, 'c', 't')")
		testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (4, 'd', 'f')")
		testhelper.AssertQueryRuns(connectionPool, "ANALYZE public.foo")
	})
	AfterEach(func() {
		testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.foo")
	})
	Describe("GetAttributeStatistics", func() {
		It("returns attribute statistics for a table", func() {
			attStats := backup.GetAttributeStatistics(connectionPool, tables)
			Expect(attStats).To(HaveLen(1))
			Expect(attStats[tableOid]).To(HaveLen(3))

			// ANALYZE can choose different slots and values depending on the database
			// version and segment layout. Compare the query result with the catalog,
			// while keeping explicit expectations for the column metadata.
			catalogStats := []backup.AttributeStatistic{}
			err := connectionPool.SelectWithArgs(&catalogStats, `
				SELECT * FROM pg_catalog.pg_statistic
				WHERE starelid = $1 ORDER BY staattnum`, tableOid)
			Expect(err).ToNot(HaveOccurred())
			Expect(catalogStats).To(HaveLen(3))

			expectedColumns := []backup.AttributeStatistic{
				{Oid: tableOid, Schema: "public", Table: "foo", AttName: "i", Type: "int4",
					Relid: tableOid, AttNumber: 1, Width: 4, Distinct: -1},
				{Oid: tableOid, Schema: "public", Table: "foo", AttName: "j", Type: "text",
					Relid: tableOid, AttNumber: 2, Width: 2, Distinct: -1},
				{Oid: tableOid, Schema: "public", Table: "foo", AttName: "k", Type: "bool",
					Relid: tableOid, AttNumber: 3, Width: 1, Distinct: -0.5},
			}
			for i := range expectedColumns {
				actual := attStats[tableOid][i]
				structmatcher.ExpectStructsToMatchIncluding(&expectedColumns[i], &actual,
					"Oid", "Schema", "Table", "AttName", "Type", "Relid", "AttNumber",
					"Inherit", "NullFraction", "Width", "Distinct")
				structmatcher.ExpectStructsToMatchExcluding(&catalogStats[i], &actual,
					"Oid", "Schema", "Table", "AttName", "Type")
			}

		})
	})
	Describe("GetTupleStatistics", func() {
		It("returns tuple statistics for a table", func() {
			tupleStats := backup.GetTupleStatistics(connectionPool, tables)
			Expect(tupleStats).To(HaveLen(1))
			tableTupleStats := tupleStats[tableOid]

			// Tuple statistics will not vary by GPDB version. Relpages may vary based on the hardware.
			expectedStats := backup.TupleStatistic{Oid: tableOid, Schema: "public", Table: "foo", RelTuples: 4}

			structmatcher.ExpectStructsToMatchExcluding(&expectedStats, &tableTupleStats, "RelPages")
		})
	})
})
