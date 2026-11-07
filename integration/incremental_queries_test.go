package integration

import (
	"fmt"

	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/options"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-go-libs/dbconn"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup integration tests", func() {
	const (
		dropTableSQL  = "DROP TABLE %s"
		addColumnSQL  = "ALTER TABLE %s ADD COLUMN k int DEFAULT 0"
		dropColumnSQL = "ALTER TABLE %s DROP COLUMN k"
		insertSQL     = "INSERT INTO %s values(10)"
		deleteSQL     = "DELETE FROM %s"
	)

	var aoTableFQN = "public.ao_foo"
	var aoCOTableFQN = "public.ao_co_foo"
	var aoPartParentTableFQN = "public.ao_part"
	var aoPartChildTableFQN = "public.ao_part_1_prt_child"
	BeforeEach(func() {
		testhelper.AssertQueryRuns(connectionPool,
			fmt.Sprintf("CREATE TABLE %s (i int) WITH (appendonly=true)", aoTableFQN))
		testhelper.AssertQueryRuns(connectionPool,
			fmt.Sprintf("CREATE TABLE %s (i int) WITH (appendonly=true,orientation='column')", aoCOTableFQN))
		testhelper.AssertQueryRuns(connectionPool,
			fmt.Sprintf(`CREATE TABLE %s (i int) WITH (appendonly=true)
	DISTRIBUTED BY (i)
	PARTITION BY LIST (i)
	(
		PARTITION child VALUES (10)
	);`, aoPartParentTableFQN))
	})
	AfterEach(func() {
		testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropTableSQL, aoTableFQN))
		testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropTableSQL, aoCOTableFQN))
		testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropTableSQL, aoPartParentTableFQN))
	})
	Describe("GetAOIncrementalMetadata", func() {
		Context("AO, AO_CO and AO partition tables are only just created", func() {
			var aoIncrementalMetadata map[string]toc.AOEntry
			BeforeEach(func() {
				aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
			})
			It("should have a modcount of 0", func() {
				Expect(aoIncrementalMetadata[aoTableFQN].Modcount).To(Equal(int64(0)))
				Expect(aoIncrementalMetadata[aoCOTableFQN].Modcount).To(Equal(int64(0)))
				Expect(aoIncrementalMetadata[aoPartParentTableFQN].Modcount).To(Equal(int64(0)))
				Expect(aoIncrementalMetadata[aoPartChildTableFQN].Modcount).To(Equal(int64(0)))
			})
			It("should have a last DDL timestamp", func() {
				Expect(aoIncrementalMetadata[aoTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
				Expect(aoIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
				Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))

				// For GPDB 7+, the root partition is not included.
				// TODO: Should the root be included?
				if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
				}

			})
		})
		Context("AO, AO_CO and AO partition tables have DML changes", func() {
			Context("After an insert(s)", func() {
				var initialAOIncrementalMetadata map[string]toc.AOEntry
				var aoIncrementalMetadata map[string]toc.AOEntry
				BeforeEach(func() {
					initialAOIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoPartParentTableFQN))

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
				})
				AfterEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, aoPartParentTableFQN))
				})
				It("should increase modcount for non partition tables", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].Modcount).
						To(BeNumerically(">", initialAOIncrementalMetadata[aoTableFQN].Modcount))
					Expect(aoIncrementalMetadata[aoCOTableFQN].Modcount).
						To(BeNumerically(">", initialAOIncrementalMetadata[aoCOTableFQN].Modcount))
				})
				It("should NOT increase modcount for parent partition tables", func() {
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].Modcount).
						To(Equal(initialAOIncrementalMetadata[aoPartParentTableFQN].Modcount))
				})
				It("should increase modcount for modified child partition tables", func() {
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].Modcount).
						To(BeNumerically(">", initialAOIncrementalMetadata[aoPartChildTableFQN].Modcount))
				})
				It("should have a last DDL timestamp", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
					Expect(aoIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))

					// For GPDB 7+, the root partition is not included.
					// TODO: Should the root be included?
					if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
						Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
					}
				})
			})
			Context("After a delete operation", func() {
				var initialAOIncrementalMetadata map[string]toc.AOEntry
				var aoIncrementalMetadata map[string]toc.AOEntry
				BeforeEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoPartParentTableFQN))

					initialAOIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, aoPartParentTableFQN))

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
				})
				It("should increase modcount for non partition tables", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].Modcount).
						To(BeNumerically(">", initialAOIncrementalMetadata[aoTableFQN].Modcount))
					Expect(aoIncrementalMetadata[aoCOTableFQN].Modcount).
						To(BeNumerically(">", initialAOIncrementalMetadata[aoCOTableFQN].Modcount))
				})
				It("should NOT increase modcount for parent partition tables", func() {
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].Modcount).
						To(Equal(initialAOIncrementalMetadata[aoPartParentTableFQN].Modcount))
				})
				It("should increase modcount for modified child partition tables", func() {
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].Modcount).
						To(BeNumerically(">", initialAOIncrementalMetadata[aoPartChildTableFQN].Modcount))
				})
				It("should have a last DDL timestamp", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
					Expect(aoIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))

					// For GPDB 7+, the root partition is not included.
					// TODO: Should the root be included?
					if connectionPool.Version.IsGPDB() && connectionPool.Version.Before("7") {
						Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
					}
				})
			})
		})
		Context("AO, AO_CO and AO partition tables have DDL changes", func() {
			var initialAOIncrementalMetadata map[string]toc.AOEntry
			var aoIncrementalMetadata map[string]toc.AOEntry
			Context("After a column add", func() {
				BeforeEach(func() {
					initialAOIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoPartParentTableFQN))

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
				})
				It("should NOT care about modcount", func() {})
				It("should have a changed last DDL timestamp", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
				})
			})
			Context("After a column drop", func() {
				BeforeEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoPartParentTableFQN))
					initialAOIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropColumnSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropColumnSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropColumnSQL, aoPartParentTableFQN))

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
				})
				It("should NOT care about modcount", func() {})
				It("should have a changed last DDL timestamp", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
				})
			})
			Context("After truncating a child partition", func() {
				BeforeEach(func() {
					initialAOIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf("TRUNCATE TABLE %s", aoPartChildTableFQN))

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
				})
				It("should NOT care about modcount", func() {})
				It("should have a changed last DDL timestamp for the child", func() {
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp)))
				})
				It("should NOT have a changed last DDL timestamp for the parent", func() {
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).
						To(Equal(initialAOIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp))
				})
			})
		})
		Context("AO, AO_CO and AO partition tables have DML and DDL changes", func() {
			var initialAOIncrementalMetadata map[string]toc.AOEntry
			var aoIncrementalMetadata map[string]toc.AOEntry
			Context("After an insert followed by an ALTER table", func() {
				BeforeEach(func() {
					initialAOIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoCOTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, aoPartParentTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, aoPartParentTableFQN))

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
				})
				It("should NOT care about modcount", func() {
					//We don't care about modcount because DDL operations can reset the modcount value
				})
				It("should have a changed last DDL timestamp", func() {
					Expect(aoIncrementalMetadata[aoTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoPartParentTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
					Expect(aoIncrementalMetadata[aoPartChildTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialAOIncrementalMetadata[aoCOTableFQN].LastDDLTimestamp)))
				})
			})
		})
		Context("Filtered backup", func() {
			var aoIncrementalMetadata map[string]toc.AOEntry
			Context("During a table-filtered backup", func() {
				It("only retrieves ao metadata for specific tables", func() {
					_ = backupCmdFlags.Set(options.INCLUDE_RELATION, aoTableFQN)
					opts, err := options.NewOptions(backupCmdFlags)
					Expect(err).NotTo(HaveOccurred())
					backup.ValidateAndProcessFilterLists(opts)

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
					Expect(aoIncrementalMetadata).To(HaveLen(1))
				})
			})
			Context("During a schema-filtered backup", func() {
				It("only retrieves ao metadata for tables in a specific schema", func() {
					testhelper.AssertQueryRuns(connectionPool, "CREATE SCHEMA testschema")
					defer testhelper.AssertQueryRuns(connectionPool, "DROP SCHEMA testschema CASCADE")
					testhelper.AssertQueryRuns(connectionPool, "CREATE TABLE testschema.ao_foo (i int) WITH (appendonly=true)")

					_ = backupCmdFlags.Set(options.INCLUDE_SCHEMA, "testschema")

					aoIncrementalMetadata = backup.GetAOIncrementalMetadata(connectionPool)
					Expect(aoIncrementalMetadata).To(HaveLen(1))
				})
			})
		})
	})
	/*
	 * PAX change detection has no modcount to lean on; it digests the table's
	 * aux relation instead.  These tests pin down the properties the digest
	 * must have, and they hold for both aux layouts (pg_manifest_<oid> and
	 * pg_pax_blocks_<oid>) because the queries are selected by aux relation
	 * name rather than by server version.
	 */
	Describe("GetPAXIncrementalMetadata", func() {
		var paxTableFQN = "public.pax_foo"
		var paxPartParentTableFQN = "public.pax_part"
		var paxPartChildTableFQN = "public.pax_part_1_prt_child"
		var initialPAXIncrementalMetadata map[string]toc.PAXEntry
		var paxIncrementalMetadata map[string]toc.PAXEntry

		BeforeEach(func() {
			testutils.SkipIfNoPAX(connectionPool)
			testhelper.AssertQueryRuns(connectionPool,
				fmt.Sprintf("CREATE TABLE %s (i int) USING pax DISTRIBUTED BY (i)", paxTableFQN))
			testhelper.AssertQueryRuns(connectionPool,
				fmt.Sprintf(`CREATE TABLE %s (i int) USING pax
	DISTRIBUTED BY (i)
	PARTITION BY LIST (i)
	(
		PARTITION child VALUES (10)
	);`, paxPartParentTableFQN))
		})
		AfterEach(func() {
			testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropTableSQL, paxTableFQN))
			testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(dropTableSQL, paxPartParentTableFQN))
		})

		Context("PAX tables are only just created", func() {
			BeforeEach(func() {
				paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
			})
			It("should have an empty marker, since the table holds no data", func() {
				Expect(paxIncrementalMetadata).To(HaveKey(paxTableFQN))
				Expect(paxIncrementalMetadata[paxTableFQN].Marker).To(BeEmpty())
				Expect(paxIncrementalMetadata[paxPartChildTableFQN].Marker).To(BeEmpty())
			})
			It("should have a last DDL timestamp", func() {
				Expect(paxIncrementalMetadata[paxTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
				Expect(paxIncrementalMetadata[paxPartChildTableFQN].LastDDLTimestamp).To(Not(BeEmpty()))
			})
			It("should not track the root of a partitioned table, which has no aux relation", func() {
				Expect(paxIncrementalMetadata).To(Not(HaveKey(paxPartParentTableFQN)))
			})
		})

		Context("PAX tables have DML changes", func() {
			Context("After an insert", func() {
				BeforeEach(func() {
					initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, paxTableFQN))
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, paxPartParentTableFQN))

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				})
				It("should change the marker of the inserted-into table", func() {
					Expect(paxIncrementalMetadata[paxTableFQN].Marker).
						To(Not(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker)))
					Expect(paxIncrementalMetadata[paxTableFQN].Marker).To(Not(BeEmpty()))
				})
				It("should change the marker of the modified child partition", func() {
					Expect(paxIncrementalMetadata[paxPartChildTableFQN].Marker).
						To(Not(Equal(initialPAXIncrementalMetadata[paxPartChildTableFQN].Marker)))
				})
			})
			Context("After a read-only query", func() {
				BeforeEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, paxTableFQN))
					initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf("SELECT count(*) FROM %s", paxTableFQN))

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				})
				/*
				 * The whole point of incremental backup: reading a table must
				 * not make it look modified, or every backup degrades to a
				 * full one.
				 */
				It("should NOT change the marker", func() {
					Expect(paxIncrementalMetadata[paxTableFQN].Marker).
						To(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker))
				})
			})
			Context("After a delete", func() {
				BeforeEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, paxTableFQN))
					initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(deleteSQL, paxTableFQN))

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				})
				/*
				 * A delete adds no micro-partition, so a marker derived from
				 * the block sequence alone would miss it.  It does rewrite the
				 * visimap, which is what the digest picks up.
				 */
				It("should change the marker", func() {
					Expect(paxIncrementalMetadata[paxTableFQN].Marker).
						To(Not(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker)))
				})
			})
			Context("After an update", func() {
				BeforeEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, paxTableFQN))
					initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool,
						fmt.Sprintf("UPDATE %s SET i = 20 WHERE i = 10", paxTableFQN))

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				})
				It("should change the marker", func() {
					Expect(paxIncrementalMetadata[paxTableFQN].Marker).
						To(Not(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker)))
				})
			})
		})

		/*
		 * The marker is a digest, so these two properties are what make it
		 * safe to compare and cheap to retain: it must not vary between
		 * identical reads of the same state, and its size must not follow the
		 * size of the table, since one is held per PAX table in the backup set.
		 */
		Context("Marker properties", func() {
			BeforeEach(func() {
				testhelper.AssertQueryRuns(connectionPool,
					fmt.Sprintf("INSERT INTO %s SELECT generate_series(1, 5000)", paxTableFQN))
			})
			It("computes the same marker on every call when nothing has changed", func() {
				initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				Expect(initialPAXIncrementalMetadata[paxTableFQN].Marker).To(Not(BeEmpty()))

				for i := 0; i < 3; i++ {
					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
					Expect(paxIncrementalMetadata[paxTableFQN].Marker).
						To(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker))
				}
			})
			/*
			 * The aux relation is an ordinary heap, so its file is rewritten
			 * by VACUUM, checkpoints and hint-bit flushes with no change to
			 * any micro-partition.  A marker that followed the file's
			 * modification time would call the table modified here and force
			 * a full re-backup on every autovacuum; the digest is built from
			 * tuple contents precisely so that it does not.
			 */
			It("does not change when the aux relation's file is rewritten", func() {
				initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				Expect(initialPAXIncrementalMetadata[paxTableFQN].Marker).To(Not(BeEmpty()))

				auxTableFQN := dbconn.MustSelectString(connectionPool, fmt.Sprintf(`
					SELECT quote_ident(aux_n.nspname) || '.' || quote_ident(aux_c.relname) AS string
					FROM pg_class aux_c
						JOIN pg_namespace aux_n ON aux_c.relnamespace = aux_n.oid
					WHERE aux_n.nspname = 'pg_ext_aux'
						AND aux_c.relkind = 'r'
						AND aux_c.relname::text IN ('pg_manifest_' || '%s'::regclass::oid::text,
							'pg_pax_blocks_' || '%s'::regclass::oid::text)`, paxTableFQN, paxTableFQN))
				Expect(auxTableFQN).To(Not(BeEmpty()))

				testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf("VACUUM %s", auxTableFQN))

				paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				Expect(paxIncrementalMetadata[paxTableFQN].Marker).
					To(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker))
			})
			It("keeps a fixed marker width as the table grows", func() {
				initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				// Hex-encoded SHA-256.
				Expect(initialPAXIncrementalMetadata[paxTableFQN].Marker).To(HaveLen(64))

				testhelper.AssertQueryRuns(connectionPool,
					fmt.Sprintf("INSERT INTO %s SELECT generate_series(1, 500000)", paxTableFQN))

				paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				Expect(paxIncrementalMetadata[paxTableFQN].Marker).To(HaveLen(64))
				Expect(paxIncrementalMetadata[paxTableFQN].Marker).
					To(Not(Equal(initialPAXIncrementalMetadata[paxTableFQN].Marker)))
			})
		})

		Context("PAX tables have DDL changes", func() {
			Context("After a column add", func() {
				BeforeEach(func() {
					initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(addColumnSQL, paxTableFQN))

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				})
				It("should have a changed last DDL timestamp", func() {
					Expect(paxIncrementalMetadata[paxTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialPAXIncrementalMetadata[paxTableFQN].LastDDLTimestamp)))
				})
			})
			Context("After truncating a child partition", func() {
				BeforeEach(func() {
					testhelper.AssertQueryRuns(connectionPool, fmt.Sprintf(insertSQL, paxPartParentTableFQN))
					initialPAXIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)

					testhelper.AssertQueryRuns(connectionPool,
						fmt.Sprintf("TRUNCATE TABLE %s", paxPartChildTableFQN))

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
				})
				It("should have a changed last DDL timestamp for the child", func() {
					Expect(paxIncrementalMetadata[paxPartChildTableFQN].LastDDLTimestamp).
						To(Not(Equal(initialPAXIncrementalMetadata[paxPartChildTableFQN].LastDDLTimestamp)))
				})
			})
		})

		Context("Filtered backup", func() {
			Context("During a table-filtered backup", func() {
				It("only retrieves pax metadata for specific tables", func() {
					_ = backupCmdFlags.Set(options.INCLUDE_RELATION, paxTableFQN)
					opts, err := options.NewOptions(backupCmdFlags)
					Expect(err).NotTo(HaveOccurred())
					backup.ValidateAndProcessFilterLists(opts)

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
					Expect(paxIncrementalMetadata).To(HaveLen(1))
					Expect(paxIncrementalMetadata).To(HaveKey(paxTableFQN))
				})
			})
			Context("During a schema-filtered backup", func() {
				It("only retrieves pax metadata for tables in a specific schema", func() {
					testhelper.AssertQueryRuns(connectionPool, "CREATE SCHEMA testschema")
					defer testhelper.AssertQueryRuns(connectionPool, "DROP SCHEMA testschema CASCADE")
					testhelper.AssertQueryRuns(connectionPool,
						"CREATE TABLE testschema.pax_foo (i int) USING pax DISTRIBUTED BY (i)")

					_ = backupCmdFlags.Set(options.INCLUDE_SCHEMA, "testschema")

					paxIncrementalMetadata = backup.GetPAXIncrementalMetadata(connectionPool)
					Expect(paxIncrementalMetadata).To(HaveLen(1))
					Expect(paxIncrementalMetadata).To(HaveKey("testschema.pax_foo"))
				})
			})
		})
	})
})
