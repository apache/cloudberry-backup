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
	BeforeEach(func() {
		tocfile, backupfile = testutils.InitializeTestTOC(buffer, "predata")
	})
	Describe("PrintStatisticsStatementsForTable", func() {
		It("restores fractional segment NDV statistics in the fifth slot", func() {
			if !connectionPool.Version.IsCBDB() || connectionPool.Version.Before("2.1.0") {
				Skip("Segment NDV statistics require Cloudberry 2.1.0 or later")
			}
			testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.foo(k bool)")
			defer testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.foo")
			tableOid := testutils.OidFromObjectName(connectionPool, "public", "foo", backup.TYPE_RELATION)
			table := backup.Table{Relation: backup.Relation{Oid: tableOid, Schema: "public", Name: "foo"}}
			expected := backup.AttributeStatistic{
				Oid: tableOid, Schema: "public", Table: "foo", AttName: "k", Type: "bool",
				Relid: tableOid, AttNumber: 1, Width: 1,
				Kind5: backup.STATISTIC_KIND_NDV_BY_SEGMENTS, Operator5: 58,
				Values5: []string{"3.0000000596046448"},
			}

			for _, query := range backup.GenerateAttributeStatisticsQueries(table, expected) {
				testhelper.AssertQueryRuns(connectionPool, query)
			}

			actual := backup.GetAttributeStatistics(connectionPool, []backup.Table{table})
			Expect(actual[tableOid]).To(HaveLen(1))
			structmatcher.ExpectStructsToMatch(&expected, &actual[tableOid][0])
		})
		It("prints attribute and tuple statistics for a table", func() {
			tables := []backup.Table{
				{Relation: backup.Relation{SchemaOid: 2200, Schema: "public", Name: "foo"}},
			}

			// Create and ANALYZE a table to generate statistics
			testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.foo(i int, j text, k bool)")
			defer testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.foo")
			testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (1, 'a', 't')")
			testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.foo VALUES (2, 'b', 'f')")
			testhelper.AssertQueryRuns(connectionPool, "ANALYZE public.foo")

			oldTableOid := testutils.OidFromObjectName(connectionPool, "public", "foo", backup.TYPE_RELATION)
			tables[0].Oid = oldTableOid

			beforeAttStats := backup.GetAttributeStatistics(connectionPool, tables)
			beforeTupleStats := backup.GetTupleStatistics(connectionPool, tables)
			beforeTupleStat := beforeTupleStats[oldTableOid]

			// Drop and recreate the table to clear the statistics
			testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.foo")
			testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.foo(i int, j text, k bool)")

			// Reload the retrieved statistics into the new table
			backup.PrintStatisticsStatements(backupfile, tocfile, tables, beforeAttStats, beforeTupleStats)
			testhelper.AssertQueryRuns(connectionPool, buffer.String())

			newTableOid := testutils.OidFromObjectName(connectionPool, "public", "foo", backup.TYPE_RELATION)
			tables[0].Oid = newTableOid
			afterAttStats := backup.GetAttributeStatistics(connectionPool, tables)
			afterTupleStats := backup.GetTupleStatistics(connectionPool, tables)
			afterTupleStat := afterTupleStats[newTableOid]

			oldAtts := beforeAttStats[oldTableOid]
			newAtts := afterAttStats[newTableOid]

			// Ensure the statistics match
			Expect(afterTupleStats).To(HaveLen(len(beforeTupleStats)))
			structmatcher.ExpectStructsToMatchExcluding(&beforeTupleStat, &afterTupleStat, "Oid")
			Expect(oldAtts).To(HaveLen(3))
			Expect(newAtts).To(HaveLen(3))
			for i := range oldAtts {
				structmatcher.ExpectStructsToMatchExcluding(&oldAtts[i], &newAtts[i], "Oid", "Relid")
			}
		})
		It("prints attribute and tuple statistics for a quoted table", func() {
			tables := []backup.Table{
				{Relation: backup.Relation{SchemaOid: 2200, Schema: "public", Name: "\"foo'\"\"''bar\""}},
			}

			// Create and ANALYZE the tables to generate statistics
			testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.\"foo'\"\"''bar\"(i int, j text, k bool)")
			defer testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.\"foo'\"\"''bar\"")
			testhelper.AssertQueryRuns(connectionPool, "INSERT INTO public.\"foo'\"\"''bar\" VALUES (1, 'a', 't')")
			testhelper.AssertQueryRuns(connectionPool, "ANALYZE public.\"foo'\"\"''bar\"")

			oldTableOid := testutils.OidFromObjectName(connectionPool, "public", "foo''\"''''bar", backup.TYPE_RELATION)
			tables[0].Oid = oldTableOid

			beforeAttStats := backup.GetAttributeStatistics(connectionPool, tables)
			beforeTupleStats := backup.GetTupleStatistics(connectionPool, tables)
			beforeTupleStat := beforeTupleStats[oldTableOid]

			// Drop and recreate the table to clear the statistics
			testhelper.AssertQueryRuns(connectionPool, "DROP TABLE public.\"foo'\"\"''bar\"")
			testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE public.\"foo'\"\"''bar\"(i int, j text, k bool)")

			// Reload the retrieved statistics into the new table
			backup.PrintStatisticsStatements(backupfile, tocfile, tables, beforeAttStats, beforeTupleStats)
			testhelper.AssertQueryRuns(connectionPool, buffer.String())

			newTableOid := testutils.OidFromObjectName(connectionPool, "public", "foo''\"''''bar", backup.TYPE_RELATION)
			tables[0].Oid = newTableOid
			afterAttStats := backup.GetAttributeStatistics(connectionPool, tables)
			afterTupleStats := backup.GetTupleStatistics(connectionPool, tables)
			afterTupleStat := afterTupleStats[newTableOid]

			oldAtts := beforeAttStats[oldTableOid]
			newAtts := afterAttStats[newTableOid]

			// Ensure the statistics match
			Expect(afterTupleStats).To(HaveLen(len(beforeTupleStats)))
			structmatcher.ExpectStructsToMatchExcluding(&beforeTupleStat, &afterTupleStat, "Oid")
			Expect(oldAtts).To(HaveLen(3))
			Expect(newAtts).To(HaveLen(3))
			for i := range oldAtts {
				structmatcher.ExpectStructsToMatchExcluding(&oldAtts[i], &newAtts[i], "Oid", "Relid")
			}
		})
	})
})
