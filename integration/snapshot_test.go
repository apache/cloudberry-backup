package integration

import (
	"fmt"
	"time"

	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/spf13/pflag"

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

var _ = Describe("lock before snapshot integration", func() {
	var reader *dbconn.DBConn
	var opts *options.Options
	var readerPID int
	BeforeEach(func() {
		if !connectionPool.Version.IsCBDB() {
			Skip("lock-before-snapshot preparation requires Cloudberry")
		}
		connectionPool.MustExec("CREATE SCHEMA snapshot_prepare; CREATE TABLE snapshot_prepare.t(id int) DISTRIBUTED BY(id); INSERT INTO snapshot_prepare.t VALUES(1),(2),(3)")
		reader = dbconn.NewDBConnFromEnvironment("testdb")
		reader.MustConnect(2)
		backup.SetConnection(reader)
		backup.InitializeMetadataParams(reader)
		for i := 0; i < 2; i++ {
			reader.MustExec("SET application_name TO 'snapshot_prepare_test'", i)
			backup.SetSessionGUCs(i)
		}
		Expect(reader.Get(&readerPID, "SELECT pg_backend_pid()")).To(Succeed())
		Expect(backupCmdFlags.Set(options.INCLUDE_SCHEMA, "snapshot_prepare")).To(Succeed())
		var err error
		opts, err = options.NewOptions(backupCmdFlags)
		Expect(err).NotTo(HaveOccurred())
	})
	AfterEach(func() {
		if reader != nil {
			for i := 0; i < reader.NumConns; i++ {
				if reader.Tx[i] != nil {
					reader.MustRollback(i)
				}
			}
			reader.Close()
			reader = nil
		}
		backup.SetConnection(connectionPool)
		backup.InitializeMetadataParams(connectionPool)
		backup.SetBackupSnapshot("")
		backup.SetFilterRelationClause("")
		connectionPool.MustExec("DROP SCHEMA IF EXISTS snapshot_prepare CASCADE")
	})
	prepare := func() <-chan error {
		ready := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			ready <- backup.PrepareBackupSnapshot(opts)
		}()
		return ready
	}
	waitForLock := func(table string) {
		Eventually(func() (int, error) {
			var count int
			err := connectionPool.Get(&count, fmt.Sprintf("SELECT count(*) FROM pg_locks WHERE pid=%d AND relation='%s'::regclass AND NOT granted", readerPID, table))
			return count, err
		}, 30*time.Second, 50*time.Millisecond).Should(BeNumerically(">", 0))
	}
	It("includes a TRUNCATE and related DML committed while locks wait, on every worker", func() {
		connectionPool.MustExec("CREATE TABLE snapshot_prepare.marker(id int) DISTRIBUTED BY(id); INSERT INTO snapshot_prepare.marker VALUES(1)")
		writer := testutils.SetupTestDbConn("testdb")
		defer writer.Close()
		writer.MustBegin()
		defer func() {
			if writer.Tx[0] != nil {
				writer.MustRollback()
			}
		}()
		writer.MustExec("TRUNCATE snapshot_prepare.t; INSERT INTO snapshot_prepare.t VALUES(4),(5),(6); UPDATE snapshot_prepare.marker SET id=2")
		Expect(backupCmdFlags.Set(options.SNAPSHOT_ATTEMPTS, "1")).To(Succeed())
		ready := prepare()
		waitForLock("snapshot_prepare.t")
		writer.MustCommit()
		Eventually(ready, 30*time.Second).Should(Receive(BeNil()))
		connectionPool.MustExec("INSERT INTO snapshot_prepare.t VALUES(7); UPDATE snapshot_prepare.marker SET id=3")
		for i := 0; i < 2; i++ {
			var rows []int
			Expect(reader.Select(&rows, "SELECT id FROM snapshot_prepare.t ORDER BY id", i)).To(Succeed())
			Expect(rows).To(Equal([]int{4, 5, 6}))
			var marker int
			Expect(reader.Get(&marker, "SELECT id FROM snapshot_prepare.marker", i)).To(Succeed())
			Expect(marker).To(Equal(2))
		}
	})
	DescribeTable("retries only when the final snapshot contains an unprotected table", func(limit int) {
		Expect(backupCmdFlags.Set(options.SNAPSHOT_ATTEMPTS, fmt.Sprint(limit))).To(Succeed())
		gate := testutils.SetupTestDbConn("testdb")
		defer gate.Close()
		gate.MustBegin()
		defer func() {
			if gate.Tx[0] != nil {
				gate.MustRollback()
			}
		}()
		gate.MustExec("LOCK TABLE snapshot_prepare.t IN ACCESS EXCLUSIVE MODE")
		ready := prepare()
		waitForLock("snapshot_prepare.t")
		connectionPool.MustExec("CREATE TABLE snapshot_prepare.new_table(id int) DISTRIBUTED BY(id); INSERT INTO snapshot_prepare.new_table VALUES(9)")
		gate.MustCommit()
		if limit == 1 {
			var err error
			Eventually(ready, 30*time.Second).Should(Receive(&err))
			Expect(err).To(MatchError(ContainSubstring("snapshot_prepare.new_table")))
			Expect(reader.Tx[0]).To(BeNil())
			Expect(reader.Tx[1]).To(BeNil())
		} else {
			Eventually(ready, 30*time.Second).Should(Receive(BeNil()))
			for i := 0; i < 2; i++ {
				var value int
				Expect(reader.Get(&value, "SELECT id FROM snapshot_prepare.new_table", i)).To(Succeed())
				Expect(value).To(Equal(9))
			}
		}
	}, Entry("fails without retries", 1), Entry("locks the new table in a fresh transaction", 3))
	It("rebuilds explicit filters and partition expansion after a leaf is replaced", func() {
		connectionPool.MustExec(`CREATE TABLE snapshot_prepare.root(id int) DISTRIBUTED BY(id) PARTITION BY RANGE(id);
			CREATE TABLE snapshot_prepare.old_leaf PARTITION OF snapshot_prepare.root FOR VALUES FROM(0) TO(10)`)
		Expect(backupCmdFlags.Lookup(options.INCLUDE_SCHEMA).Value.(pflag.SliceValue).Replace(nil)).To(Succeed())
		Expect(backupCmdFlags.Set(options.INCLUDE_RELATION, "snapshot_prepare.t")).To(Succeed())
		Expect(backupCmdFlags.Set(options.INCLUDE_RELATION, "snapshot_prepare.root")).To(Succeed())
		Expect(backupCmdFlags.Set(options.LEAF_PARTITION_DATA, "true")).To(Succeed())
		var err error
		opts, err = options.NewOptions(backupCmdFlags)
		Expect(err).NotTo(HaveOccurred())
		gate := testutils.SetupTestDbConn("testdb")
		defer gate.Close()
		gate.MustBegin()
		defer func() {
			if gate.Tx[0] != nil {
				gate.MustRollback()
			}
		}()
		gate.MustExec("LOCK TABLE snapshot_prepare.t IN ACCESS EXCLUSIVE MODE")
		ready := prepare()
		waitForLock("snapshot_prepare.t")
		connectionPool.MustExec(`ALTER TABLE snapshot_prepare.root DETACH PARTITION snapshot_prepare.old_leaf;
			DROP TABLE snapshot_prepare.old_leaf;
			CREATE TABLE snapshot_prepare.new_leaf PARTITION OF snapshot_prepare.root FOR VALUES FROM(0) TO(10);
			INSERT INTO snapshot_prepare.root VALUES(4),(5),(6)`)
		gate.MustCommit()
		Eventually(ready, 30*time.Second).Should(Receive(BeNil()))
		Expect(opts.GetIncludedTables()).To(ContainElement("snapshot_prepare.new_leaf"))
		Expect(opts.GetIncludedTables()).NotTo(ContainElement("snapshot_prepare.old_leaf"))
		for i := 0; i < 2; i++ {
			var rows []int
			Expect(reader.Select(&rows, "SELECT id FROM snapshot_prepare.root ORDER BY id", i)).To(Succeed())
			Expect(rows).To(Equal([]int{4, 5, 6}))
		}
	})
	It("checks nested descendants even when only a root is supplied", func() {
		connectionPool.MustExec(`CREATE TABLE snapshot_prepare.root(id int) DISTRIBUTED BY(id) PARTITION BY RANGE(id);
            CREATE TABLE snapshot_prepare.middle PARTITION OF snapshot_prepare.root FOR VALUES FROM(0) TO(10) PARTITION BY RANGE(id);
            CREATE TABLE snapshot_prepare.leaf PARTITION OF snapshot_prepare.middle FOR VALUES FROM(0) TO(10)`)
		var oid uint32
		Expect(reader.Get(&oid, "SELECT 'snapshot_prepare.root'::regclass::oid")).To(Succeed())
		reader.MustBegin()
		reader.MustExec("LOCK TABLE ONLY snapshot_prepare.root IN ACCESS SHARE MODE")
		tables := []backup.Relation{{Oid: oid, Schema: "snapshot_prepare", Name: "root"}}
		conflicts, err := backup.CheckSnapshotTableLocks(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(ConsistOf("snapshot_prepare.middle", "snapshot_prepare.leaf"))
		reader.MustRollback()
		reader.MustBegin()
		Expect(backup.LockTables(reader, tables)).To(Succeed())
		conflicts, err = backup.CheckSnapshotTableLocks(reader, tables)
		Expect(err).NotTo(HaveOccurred())
		Expect(conflicts).To(BeEmpty())
	})
})
