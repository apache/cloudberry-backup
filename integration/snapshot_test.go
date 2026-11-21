package integration

import (
	"fmt"

	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/spf13/cobra"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("synchronized snapshot integration tests", func() {
	BeforeEach(func() {
		if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("6.21.0") {
			Skip("snapshot feature not supported")
		}
	})
	Describe("GetSynchronizedSnapshot", func() {
		It("returns a correctly formatted snapshot identifier", func() {
			snapshotId, err := backup.GetSynchronizedSnapshot(connectionPool)
			Expect(err).ToNot(HaveOccurred())
			Expect(snapshotId).ToNot(BeEmpty())
			Expect(snapshotId).To(MatchRegexp("[0-9a-fA-F]+-\\d"))
		})
		It("returns a valid snapshot identifier that can be used to set a snapshot", func() {
			exportConn := testutils.SetupTestDbConn("testdb")
			importConn := testutils.SetupTestDbConn("testdb")
			exportConn.MustBegin()
			importConn.MustBegin()
			snapshotId, err := backup.GetSynchronizedSnapshot(exportConn)
			Expect(err).ToNot(HaveOccurred())

			_, err = importConn.Exec(fmt.Sprintf("SET TRANSACTION SNAPSHOT '%s'", snapshotId), 0)
			Expect(err).ToNot(HaveOccurred())

			exportConn.MustCommit()
			importConn.MustCommit()
			exportConn.Close()
			importConn.Close()
		})
	})
	Describe("SetSynchronizedSnapshot", func() {
		It("sets snapshot to snapshotId", func() {
			var snapshotId string
			exportConn := testutils.SetupTestDbConn("testdb")
			importConn := testutils.SetupTestDbConn("testdb")
			exportConn.MustBegin()
			importConn.MustBegin()
			err := exportConn.Get(&snapshotId, "SELECT pg_catalog.pg_export_snapshot()", 0)
			Expect(err).ToNot(HaveOccurred())

			err = backup.SetSynchronizedSnapshot(importConn, 0, snapshotId)
			Expect(err).ToNot(HaveOccurred())

			exportConn.MustCommit()
			importConn.MustCommit()
			exportConn.Close()
			importConn.Close()
		})
	})
	Describe("functionality tests", func() {
		BeforeEach(func() {
			connectionPool.Exec("CREATE TABLE IF NOT EXISTS snapshot_test (a int)")
			connectionPool.Exec("TRUNCATE TABLE snapshot_test")
		})
		It("handles concurrent insert", func() {
			query := "SELECT count(*) FROM snapshot_test"
			var result int
			exportConn := testutils.SetupTestDbConn("testdb")
			importConn := testutils.SetupTestDbConn("testdb")
			connectionPool.Exec("INSERT INTO snapshot_test values(1)")

			exportConn.MustBegin(0)
			snapshotId, err := backup.GetSynchronizedSnapshot(exportConn)
			Expect(err).ToNot(HaveOccurred())
			// External command inserts row
			connectionPool.Exec("INSERT INTO snapshot_test values(2)")
			connectionPool.Get(&result, query)
			Expect(result).To(Equal(2))

			// export should not see inserted row
			err = exportConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))

			// import should see inserted row
			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(2))

			importConn.MustBegin(0)
			err = backup.SetSynchronizedSnapshot(importConn, 0, snapshotId)
			// Should see snapshot with 1 row
			Expect(err).ToNot(HaveOccurred())
			importConn.Get(&result, query)
			Expect(result).To(Equal(1))

			// after commits, inserted row is visible to both conns
			err = exportConn.Commit(0)
			Expect(err).ToNot(HaveOccurred())
			err = importConn.Commit(0)
			Expect(err).ToNot(HaveOccurred())

			err = exportConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(2))

			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(2))

			exportConn.Close()
			importConn.Close()
		})
		It("handles concurrent update", func() {
			var result int
			query := "SELECT count(*) from snapshot_test WHERE a=99"
			exportConn := testutils.SetupTestDbConn("testdb")
			importConn := testutils.SetupTestDbConn("testdb")
			connectionPool.Exec("INSERT INTO snapshot_test SELECT a FROM generate_series(1,5) a")

			// export the snapshot
			exportConn.MustBegin(0)
			snapshotId, err := backup.GetSynchronizedSnapshot(exportConn)
			Expect(err).ToNot(HaveOccurred())
			Expect(snapshotId).ToNot(BeEmpty())

			// external command updates row
			_, err = connectionPool.Exec("UPDATE snapshot_test SET a=99 WHERE a=1")
			Expect(err).ToNot(HaveOccurred())
			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))

			// export should not see updated row
			err = exportConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(0))

			// import should see updated row
			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))

			// now set snapshot, import should not see updated row
			importConn.MustBegin(0)
			err = backup.SetSynchronizedSnapshot(importConn, 0, snapshotId)
			Expect(err).ToNot(HaveOccurred())
			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(0))

			// after commits, updated row is visible to both conns
			err = exportConn.Commit(0)
			Expect(err).ToNot(HaveOccurred())
			err = importConn.Commit(0)
			Expect(err).ToNot(HaveOccurred())

			err = exportConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))

			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(1))

			exportConn.Close()
			importConn.Close()
		})
		It("handles concurrent delete", func() {
			query := "SELECT count(*) FROM snapshot_test"
			var result int
			exportConn := testutils.SetupTestDbConn("testdb")
			importConn := testutils.SetupTestDbConn("testdb")
			connectionPool.Exec("INSERT INTO snapshot_test SELECT a FROM generate_series(1,5) a")

			exportConn.MustBegin(0)
			snapshotId, err := backup.GetSynchronizedSnapshot(exportConn)
			Expect(err).ToNot(HaveOccurred())
			// External command deletes row
			connectionPool.Exec("DELETE FROM snapshot_test where a=1")
			connectionPool.Get(&result, query)
			Expect(result).To(Equal(4))

			// export should see all 5 rows
			err = exportConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(5))

			// import should see 4 rows
			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(4))

			importConn.MustBegin(0)
			err = backup.SetSynchronizedSnapshot(importConn, 0, snapshotId)
			// Should see snapshot with 5 rows
			Expect(err).ToNot(HaveOccurred())
			importConn.Get(&result, query)
			Expect(result).To(Equal(5))

			// after commits, deleted row is not visible to either conn
			err = exportConn.Commit(0)
			Expect(err).ToNot(HaveOccurred())
			err = importConn.Commit(0)
			Expect(err).ToNot(HaveOccurred())

			err = exportConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(4))

			err = importConn.Get(&result, query)
			Expect(err).ToNot(HaveOccurred())
			Expect(result).To(Equal(4))

			exportConn.Close()
			importConn.Close()
		})
	})
})

var _ = Describe("snapshot storage validation", func() {
	BeforeEach(func() {
		if !connectionPool.Version.IsCBDB() {
			Skip("snapshot storage validation is supported only on Cloudberry")
		}
		connectionPool.MustExec("CREATE TABLE public.snapshot_storage (id int) DISTRIBUTED BY (id); INSERT INTO public.snapshot_storage VALUES (1),(2),(3)")
	})
	AfterEach(func() { connectionPool.MustExec("DROP TABLE public.snapshot_storage") })
	It("detects a committed rewrite and accepts a fresh shared snapshot", func() {
		reader := testutils.SetupTestDbConn("testdb")
		reader.MustExec("SET search_path TO pg_catalog")
		defer reader.Close()
		worker := testutils.SetupTestDbConn("testdb")
		defer worker.Close()
		reader.MustBegin()
		_, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		var oid uint32
		Expect(reader.Get(&oid, "SELECT 'public.snapshot_storage'::regclass::oid")).To(Succeed())
		tables := []backup.Relation{{Oid: oid, Schema: "public", Name: "snapshot_storage"}}
		var before, after []string
		query := "SELECT gp_segment_id::text || ':' || relfilenode::text FROM gp_dist_random('pg_class') WHERE oid='public.snapshot_storage'::regclass ORDER BY gp_segment_id"
		Expect(connectionPool.Select(&before, query)).To(Succeed())
		connectionPool.MustExec("TRUNCATE public.snapshot_storage; INSERT INTO public.snapshot_storage VALUES (4),(5),(6)")
		Expect(connectionPool.Select(&after, query)).To(Succeed())
		Expect(after).NotTo(Equal(before))
		Expect(backup.LockTables(reader, tables)).To(Succeed())
		conflicts, err := backup.CheckSnapshotTables(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(ConsistOf(ContainSubstring("storage changed")))
		reader.MustRollback()
		reader.MustBegin()
		snapshot, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		Expect(backup.SetSynchronizedSnapshot(worker, 0, snapshot)).To(Succeed())
		Expect(backup.LockTables(reader, tables)).To(Succeed())
		conflicts, err = backup.CheckSnapshotTables(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(BeEmpty())
		for _, conn := range []*dbconn.DBConn{reader, worker} {
			var rows []int
			Expect(conn.Select(&rows, "SELECT id FROM public.snapshot_storage ORDER BY id")).To(Succeed())
			Expect(rows).To(Equal([]int{4, 5, 6}))
			conn.MustRollback()
		}
	})
	It("does not reject ordinary concurrent DML", func() {
		reader := testutils.SetupTestDbConn("testdb")
		reader.MustExec("SET search_path TO pg_catalog")
		defer reader.Close()
		reader.MustBegin()
		defer reader.MustRollback()
		_, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		var oid uint32
		Expect(reader.Get(&oid, "SELECT 'public.snapshot_storage'::regclass::oid")).To(Succeed())
		connectionPool.MustExec("INSERT INTO public.snapshot_storage VALUES (4); UPDATE public.snapshot_storage SET id=8 WHERE id=2; DELETE FROM public.snapshot_storage WHERE id=3")
		tables := []backup.Relation{{Oid: oid, Schema: "public", Name: "snapshot_storage"}}
		Expect(backup.LockTables(reader, tables)).To(Succeed())
		conflicts, err := backup.CheckSnapshotTables(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(BeEmpty())
		var rows []int
		Expect(reader.Select(&rows, "SELECT id FROM public.snapshot_storage ORDER BY id")).To(Succeed())
		Expect(rows).To(Equal([]int{1, 2, 3}))
	})
	It("detects a renamed table replaced under its old name", func() {
		defer connectionPool.MustExec("DROP TABLE IF EXISTS public.snapshot_old")
		reader := testutils.SetupTestDbConn("testdb")
		reader.MustExec("SET search_path TO pg_catalog")
		defer reader.Close()
		reader.MustBegin()
		defer reader.MustRollback()
		_, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		var oid uint32
		Expect(reader.Get(&oid, "SELECT oid FROM pg_class WHERE relname='snapshot_storage'")).To(Succeed())
		connectionPool.MustExec("ALTER TABLE public.snapshot_storage RENAME TO snapshot_old; CREATE TABLE public.snapshot_storage (id int) DISTRIBUTED BY (id)")
		tables := []backup.Relation{{Oid: oid, Schema: "public", Name: "snapshot_storage"}}
		Expect(backup.LockTables(reader, tables)).To(Succeed())
		conflicts, err := backup.CheckSnapshotTables(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(ConsistOf(ContainSubstring("snapshot OID")))
	})
	It("checks a nested leaf when only the root is supplied", func() {
		connectionPool.MustExec(`CREATE TABLE public.snapshot_root (id int) PARTITION BY RANGE(id) DISTRIBUTED BY(id);
   CREATE TABLE public.snapshot_middle PARTITION OF public.snapshot_root FOR VALUES FROM (0) TO (10) PARTITION BY RANGE(id);
   CREATE TABLE public.snapshot_leaf PARTITION OF public.snapshot_middle FOR VALUES FROM (0) TO (10);
   INSERT INTO public.snapshot_root VALUES(1),(2),(3)`)
		defer connectionPool.MustExec("DROP TABLE public.snapshot_root")
		reader := testutils.SetupTestDbConn("testdb")
		reader.MustExec("SET search_path TO pg_catalog")
		defer reader.Close()
		reader.MustBegin()
		defer reader.MustRollback()
		_, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		var oid uint32
		Expect(reader.Get(&oid, "SELECT oid FROM pg_class WHERE relname='snapshot_root'")).To(Succeed())
		connectionPool.MustExec("TRUNCATE public.snapshot_leaf; INSERT INTO public.snapshot_leaf VALUES(4),(5),(6)")
		tables := []backup.Relation{{Oid: oid, Schema: "public", Name: "snapshot_root"}}
		Expect(backup.LockTables(reader, tables)).To(Succeed())
		conflicts, err := backup.CheckSnapshotTables(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(ConsistOf(ContainSubstring("public.snapshot_leaf")))
	})
})

var _ = Describe("snapshot retry filter resolution", func() {
	BeforeEach(func() {
		if !connectionPool.Version.IsCBDB() {
			Skip("snapshot retries are supported only on Cloudberry")
		}
	})
	It("resolves a replacement OID and restores session settings and worker snapshots", func() {
		connectionPool.MustExec("CREATE TABLE public.retry_filter(id int) DISTRIBUTED BY(id); INSERT INTO public.retry_filter VALUES(1)")
		defer connectionPool.MustExec("DROP TABLE public.retry_filter; DROP TABLE public.retry_filter_old")
		reader := dbconn.NewDBConnFromEnvironment("testdb")
		reader.MustConnect(2)
		defer reader.Close()
		defer func() {
			backup.SetConnection(connectionPool)
			backup.InitializeMetadataParams(connectionPool)
			backup.SetBackupSnapshot("")
		}()
		cmd := &cobra.Command{}
		backup.DoInit(cmd)
		backup.SetConnection(reader)
		backup.InitializeMetadataParams(reader)
		Expect(cmd.Flags().Set(options.INCLUDE_RELATION, "public.retry_filter")).To(Succeed())
		opts, err := options.NewOptions(cmd.Flags())
		Expect(err).NotTo(HaveOccurred())
		reader.MustBegin(0)
		reader.MustBegin(1)
		snapshot, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		backup.SetBackupSnapshot(snapshot)
		Expect(backup.SetSynchronizedSnapshot(reader, 1, snapshot)).To(Succeed())
		backup.ValidateAndProcessFilterLists(opts)
		oldOid := backup.IncludedRelationFqns[0].Oid
		connectionPool.MustExec("ALTER TABLE public.retry_filter RENAME TO retry_filter_old; CREATE TABLE public.retry_filter(id int) DISTRIBUTED BY(id); INSERT INTO public.retry_filter VALUES(4),(5),(6)")
		_, data := backup.RetrieveAndProcessTables(opts)
		Expect(data).To(HaveLen(1))
		Expect(data[0].Oid).NotTo(Equal(oldOid))
		Expect(backup.IncludedRelationFqns[0].Oid).To(Equal(data[0].Oid))
		connectionPool.MustExec("INSERT INTO public.retry_filter VALUES(7)")
		for i := 0; i < 2; i++ {
			var rows []int
			Expect(reader.Select(&rows, "SELECT id FROM public.retry_filter ORDER BY id", i)).To(Succeed())
			Expect(rows).To(Equal([]int{4, 5, 6}))
			var searchPath, appName, timeout string
			Expect(reader.Get(&searchPath, "SHOW search_path", i)).To(Succeed())
			Expect(searchPath).To(Equal("pg_catalog"))
			Expect(reader.Get(&timeout, "SHOW statement_timeout", i)).To(Succeed())
			Expect(timeout).To(Equal("0"))
			Expect(reader.Get(&appName, "SHOW application_name", i)).To(Succeed())
			Expect(appName).To(HavePrefix("gpbackup_"))
			reader.MustRollback(i)
		}
	})
	It("rejects an explicitly included table that disappeared before retry", func() {
		connectionPool.MustExec("CREATE TABLE public.retry_missing(id int) DISTRIBUTED BY(id)")
		defer connectionPool.MustExec("DROP TABLE public.retry_missing_old")
		reader := dbconn.NewDBConnFromEnvironment("testdb")
		reader.MustConnect(2)
		defer reader.Close()
		defer func() {
			backup.SetConnection(connectionPool)
			backup.InitializeMetadataParams(connectionPool)
			backup.SetBackupSnapshot("")
		}()
		cmd := &cobra.Command{}
		backup.DoInit(cmd)
		backup.SetConnection(reader)
		backup.InitializeMetadataParams(reader)
		Expect(cmd.Flags().Set(options.INCLUDE_RELATION, "public.retry_missing")).To(Succeed())
		opts, err := options.NewOptions(cmd.Flags())
		Expect(err).NotTo(HaveOccurred())
		reader.MustBegin(0)
		reader.MustBegin(1)
		defer func() {
			for i := 0; i < 2; i++ {
				if reader.Tx[i] != nil {
					reader.MustRollback(i)
				}
			}
		}()
		snapshot, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		backup.SetBackupSnapshot(snapshot)
		Expect(backup.SetSynchronizedSnapshot(reader, 1, snapshot)).To(Succeed())
		backup.ValidateAndProcessFilterLists(opts)
		connectionPool.MustExec("ALTER TABLE public.retry_missing RENAME TO retry_missing_old")
		Expect(func() { backup.RetrieveAndProcessTables(opts) }).To(PanicWith(MatchRegexp("Table public.retry_missing does not exist")))
	})

	It("discards expanded old partitions and discovers new ones after retry", func() {
		connectionPool.MustExec(`CREATE TABLE public.retry_root(id int) DISTRIBUTED BY(id) PARTITION BY RANGE(id);
   CREATE TABLE public.retry_old_leaf PARTITION OF public.retry_root FOR VALUES FROM(0) TO(10)`)
		defer connectionPool.MustExec("DROP TABLE public.retry_root")
		reader := dbconn.NewDBConnFromEnvironment("testdb")
		reader.MustConnect(2)
		defer reader.Close()
		defer func() {
			backup.SetConnection(connectionPool)
			backup.InitializeMetadataParams(connectionPool)
			backup.SetBackupSnapshot("")
		}()
		cmd := &cobra.Command{}
		backup.DoInit(cmd)
		backup.SetConnection(reader)
		backup.InitializeMetadataParams(reader)
		Expect(cmd.Flags().Set(options.INCLUDE_RELATION, "public.retry_root")).To(Succeed())
		Expect(cmd.Flags().Set(options.LEAF_PARTITION_DATA, "true")).To(Succeed())
		opts, err := options.NewOptions(cmd.Flags())
		Expect(err).NotTo(HaveOccurred())
		reader.MustBegin(0)
		reader.MustBegin(1)
		snapshot, err := backup.GetSynchronizedSnapshot(reader)
		Expect(err).NotTo(HaveOccurred())
		backup.SetBackupSnapshot(snapshot)
		Expect(backup.SetSynchronizedSnapshot(reader, 1, snapshot)).To(Succeed())
		backup.ValidateAndProcessFilterLists(opts)
		Expect(backup.ExpandIncludesForPartitions(reader, opts, backup.GetOidsFromRelationList(backup.IncludedRelationFqns), cmd.Flags())).To(Succeed())
		Expect(opts.GetIncludedTables()).To(ContainElement("public.retry_old_leaf"))
		connectionPool.MustExec(`ALTER TABLE public.retry_root DETACH PARTITION public.retry_old_leaf; DROP TABLE public.retry_old_leaf;
   CREATE TABLE public.retry_new_leaf PARTITION OF public.retry_root FOR VALUES FROM(0) TO(10); INSERT INTO public.retry_root VALUES(4),(5),(6)`)
		_, data := backup.RetrieveAndProcessTables(opts)
		Expect(data).To(HaveLen(1))
		Expect(data[0].Name).To(Equal("retry_new_leaf"))
		Expect(opts.GetIncludedTables()).NotTo(ContainElement("public.retry_old_leaf"))
		Expect(opts.GetIncludedTables()).To(ContainElement("public.retry_new_leaf"))
		for i := 0; i < 2; i++ {
			reader.MustRollback(i)
		}
	})
})
