package backup_test

import (
	"os"

	"github.com/apache/cloudberry-backup/backup"
	"github.com/apache/cloudberry-backup/filepath"
	"github.com/apache/cloudberry-backup/history"
	"github.com/apache/cloudberry-backup/report"
	"github.com/apache/cloudberry-backup/testutils"
	"github.com/apache/cloudberry-backup/toc"
	"github.com/apache/cloudberry-go-libs/operating"
	"github.com/apache/cloudberry-go-libs/structmatcher"
	"github.com/apache/cloudberry-go-libs/testhelper"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
)

var _ = Describe("backup/incremental tests", func() {
	Describe("FilterTablesForIncremental", func() {
		defaultEntry := toc.AOEntry{
			Modcount:         0,
			LastDDLTimestamp: "00000",
		}
		defaultPAXEntry := toc.PAXEntry{
			Marker:           "marker0",
			LastDDLTimestamp: "00000",
		}
		prevTOC := toc.TOC{
			IncrementalMetadata: toc.IncrementalEntries{
				AO: map[string]toc.AOEntry{
					"public.ao_changed_modcount":  defaultEntry,
					"public.ao_changed_timestamp": defaultEntry,
					"public.ao_unchanged":         defaultEntry,
					"public.ao_rewritten":         defaultEntry,
				},
				PAX: map[string]toc.PAXEntry{
					"public.pax_changed_marker":    defaultPAXEntry,
					"public.pax_changed_timestamp": defaultPAXEntry,
					"public.pax_unchanged":         defaultPAXEntry,
					"public.pax_rewritten":         defaultPAXEntry,
				},
			},
		}

		currTOC := toc.TOC{
			IncrementalMetadata: toc.IncrementalEntries{
				AO: map[string]toc.AOEntry{
					"public.ao_changed_modcount": {
						Modcount:         2,
						LastDDLTimestamp: "00000",
					},
					"public.ao_changed_timestamp": {
						Modcount:         0,
						LastDDLTimestamp: "00001",
					},
					"public.ao_unchanged": defaultEntry,
					// A rewrite leaves the modcount and the DDL timestamp
					// exactly as they were; only the storage moved.
					"public.ao_rewritten": {
						Modcount:         0,
						RelFileNode:      200,
						LastDDLTimestamp: "00000",
					},
				},
				PAX: map[string]toc.PAXEntry{
					"public.pax_changed_marker": {
						Marker:           "marker1",
						LastDDLTimestamp: "00000",
					},
					"public.pax_changed_timestamp": {
						Marker:           "marker0",
						LastDDLTimestamp: "00001",
					},
					"public.pax_unchanged": defaultPAXEntry,
					// Same here: a rewrite can reproduce an earlier digest,
					// so the storage identity is what separates them.
					"public.pax_rewritten": {
						Marker:           "marker0",
						RelFileNode:      200,
						LastDDLTimestamp: "00000",
					},
					// A PAX table created since the previous backup has no
					// entry at all in prevTOC, so it compares against the
					// zero value and must be backed up.
					"public.pax_new": {
						Marker:           "marker2",
						LastDDLTimestamp: "00002",
					},
				},
			},
		}

		tblHeap := backup.Table{Relation: backup.Relation{Schema: "public", Name: "heap"}}
		tblAOChangedModcount := backup.Table{Relation: backup.Relation{Schema: "public", Name: "ao_changed_modcount"}}
		tblAOChangedTS := backup.Table{Relation: backup.Relation{Schema: "public", Name: "ao_changed_timestamp"}}
		tblAOUnchanged := backup.Table{Relation: backup.Relation{Schema: "public", Name: "ao_unchanged"}}
		tblPAXChangedMarker := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_changed_marker"}}
		tblPAXChangedTS := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_changed_timestamp"}}
		tblPAXUnchanged := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_unchanged"}}
		tblPAXNew := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_new"}}
		tblPAXUntracked := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_untracked"}}
		tblAORewritten := backup.Table{Relation: backup.Relation{Schema: "public", Name: "ao_rewritten"}}
		tblPAXRewritten := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_rewritten"}}
		tables := []backup.Table{
			tblHeap,
			tblAOChangedModcount,
			tblAOChangedTS,
			tblAOUnchanged,
			tblAORewritten,
			tblPAXChangedMarker,
			tblPAXChangedTS,
			tblPAXUnchanged,
			tblPAXNew,
			tblPAXUntracked,
			tblPAXRewritten,
		}

		filteredTables := backup.FilterTablesForIncremental(&prevTOC, &currTOC, tables)

		It("Should include the heap table in the filtered list", func() {
			Expect(filteredTables).To(ContainElement(tblHeap))
		})

		It("Should include the AO table having a modified modcount", func() {
			Expect(filteredTables).To(ContainElement(tblAOChangedModcount))
		})

		It("Should include the AO table having a modified last DDL timestamp", func() {
			Expect(filteredTables).To(ContainElement(tblAOChangedTS))
		})

		It("Should NOT include the unmodified AO table", func() {
			Expect(filteredTables).To(Not(ContainElement(tblAOUnchanged)))
		})

		It("Should include the AO table whose storage was replaced by a rewrite", func() {
			Expect(filteredTables).To(ContainElement(tblAORewritten))
		})

		It("Should include the PAX table having a modified marker", func() {
			Expect(filteredTables).To(ContainElement(tblPAXChangedMarker))
		})

		It("Should include the PAX table having a modified last DDL timestamp", func() {
			Expect(filteredTables).To(ContainElement(tblPAXChangedTS))
		})

		It("Should NOT include the unmodified PAX table", func() {
			Expect(filteredTables).To(Not(ContainElement(tblPAXUnchanged)))
		})

		It("Should include a PAX table that did not exist in the previous backup", func() {
			Expect(filteredTables).To(ContainElement(tblPAXNew))
		})

		It("Should include a PAX table with no incremental metadata at all", func() {
			Expect(filteredTables).To(ContainElement(tblPAXUntracked))
		})

		It("Should include the PAX table whose storage was replaced by a rewrite", func() {
			Expect(filteredTables).To(ContainElement(tblPAXRewritten))
		})
	})

	Describe("FilterTablesForIncremental with a TOC predating PAX support", func() {
		// A backup taken by a version without a PAX map must not cause a PAX
		// table to be skipped, since we cannot prove it is unchanged.
		prevTOC := toc.TOC{
			IncrementalMetadata: toc.IncrementalEntries{
				AO: map[string]toc.AOEntry{},
			},
		}
		currTOC := toc.TOC{
			IncrementalMetadata: toc.IncrementalEntries{
				AO: map[string]toc.AOEntry{},
				PAX: map[string]toc.PAXEntry{
					"public.pax_table": {Marker: "marker1", LastDDLTimestamp: "00001"},
				},
			},
		}
		tblPAX := backup.Table{Relation: backup.Relation{Schema: "public", Name: "pax_table"}}

		It("Should include the PAX table", func() {
			Expect(backup.FilterTablesForIncremental(&prevTOC, &currTOC, []backup.Table{tblPAX})).
				To(ContainElement(tblPAX))
		})
	})

	Describe("GetLatestMatchingBackupConfig", func() {
		historyDBPath := "/tmp/hist.db"
		contents := []history.BackupConfig{
			{
				DatabaseName:     "test2",
				Timestamp:        "timestamp4",
				Status:           history.BackupStatusFailed,
				ExcludeRelations: []string{},
				ExcludeSchemas:   []string{},
				IncludeRelations: []string{},
				IncludeSchemas:   []string{},
				RestorePlan:      []history.RestorePlanEntry{},
			},
			{
				DatabaseName:     "test1",
				Timestamp:        "timestamp3",
				Status:           history.BackupStatusSucceed,
				ExcludeRelations: []string{},
				ExcludeSchemas:   []string{},
				IncludeRelations: []string{},
				IncludeSchemas:   []string{},
				RestorePlan:      []history.RestorePlanEntry{}},
			{
				DatabaseName:     "test2",
				Timestamp:        "timestamp2",
				Status:           history.BackupStatusSucceed,
				ExcludeRelations: []string{},
				ExcludeSchemas:   []string{},
				IncludeRelations: []string{},
				IncludeSchemas:   []string{},
				RestorePlan:      []history.RestorePlanEntry{},
			},
			{
				DatabaseName:     "test1",
				Timestamp:        "timestamp1",
				Status:           history.BackupStatusSucceed,
				ExcludeRelations: []string{},
				ExcludeSchemas:   []string{},
				IncludeRelations: []string{},
				IncludeSchemas:   []string{},
				RestorePlan:      []history.RestorePlanEntry{},
			},
		}
		BeforeEach(func() {
			os.Remove(historyDBPath)
			historyDB, _ := history.InitializeHistoryDatabase(historyDBPath)
			for _, backupConfig := range contents {
				history.StoreBackupHistory(historyDB, &backupConfig)
			}
			historyDB.Close()
		})

		AfterEach(func() {
			os.Remove(historyDBPath)
		})

		It("Should return the latest backup's timestamp with matching Dbname", func() {
			currentBackupConfig := history.BackupConfig{DatabaseName: "test1"}
			latestBackupHistoryEntry := backup.GetLatestMatchingBackupConfig(historyDBPath, &currentBackupConfig)
			// endtime is set dynamically on storage, so force it to match
			contents[1].EndTime = latestBackupHistoryEntry.EndTime
			structmatcher.ExpectStructsToMatch(contents[1], latestBackupHistoryEntry)
		})
		It("Should return the latest matching backup's timestamp that did not fail", func() {
			currentBackupConfig := history.BackupConfig{DatabaseName: "test2"}
			latestBackupHistoryEntry := backup.GetLatestMatchingBackupConfig(historyDBPath, &currentBackupConfig)
			contents[2].EndTime = latestBackupHistoryEntry.EndTime
			structmatcher.ExpectStructsToMatch(contents[2], latestBackupHistoryEntry)
		})
		It("should return nil with no matching Dbname", func() {
			currentBackupConfig := history.BackupConfig{DatabaseName: "test3"}
			latestBackupHistoryEntry := backup.GetLatestMatchingBackupConfig(historyDBPath, &currentBackupConfig)
			Expect(latestBackupHistoryEntry).To(BeNil())
		})
		It("should return nil with an empty history", func() {
			currentBackupConfig := history.BackupConfig{}
			os.Remove(historyDBPath)
			latestBackupHistoryEntry := backup.GetLatestMatchingBackupConfig(historyDBPath, &currentBackupConfig)
			Expect(latestBackupHistoryEntry).To(BeNil())
		})
	})

	Describe("PopulateRestorePlan", func() {
		testCluster := testutils.SetDefaultSegmentConfiguration()
		testFPInfo := filepath.NewFilePathInfo(testCluster, "", "ts0",
			"gpseg", false)
		backup.SetFPInfo(testFPInfo)

		Context("Full backup", func() {
			restorePlan := make([]history.RestorePlanEntry, 0)
			backupSetTables := []backup.Table{
				{Relation: backup.Relation{Schema: "public", Name: "ao1"}},
				{Relation: backup.Relation{Schema: "public", Name: "heap1"}},
			}
			allTables := backupSetTables

			restorePlan = backup.PopulateRestorePlan(backupSetTables, restorePlan, allTables)

			It("Should populate a restore plan with a single entry", func() {
				Expect(restorePlan).To(HaveLen(1))
			})

			Specify("That the single entry should have the latest timestamp", func() {
				Expect(restorePlan[0].Timestamp).To(Equal("ts0"))
			})

			Specify("That the single entry should have the current backup set FQNs", func() {
				expectedTableFQNs := []string{"public.ao1", "public.heap1"}

				Expect(restorePlan[0].TableFQNs).To(Equal(expectedTableFQNs))
			})
		})

		Context("Incremental backup", func() {
			previousRestorePlan := []history.RestorePlanEntry{
				{Timestamp: "ts0", TableFQNs: []string{"public.ao1", "public.ao2"}},
				{Timestamp: "ts1", TableFQNs: []string{"public.heap1"}},
			}
			changedTables := []backup.Table{
				{Relation: backup.Relation{Schema: "public", Name: "ao1"}},
				{Relation: backup.Relation{Schema: "public", Name: "heap1"}},
			}

			Context("Incremental backup with no table drops in between", func() {
				allTables := changedTables

				restorePlan := backup.PopulateRestorePlan(changedTables, previousRestorePlan, allTables)

				It("should append 1 more entry to the previous restore plan", func() {
					Expect(restorePlan[0:2]).To(Equal(previousRestorePlan[0:2]))
					Expect(restorePlan).To(HaveLen(len(previousRestorePlan) + 1))
				})

				Specify("That the added entry should have the current backup set FQNs", func() {
					expectedTableFQNs := []string{"public.ao1", "public.heap1"}

					Expect(restorePlan[2].TableFQNs).To(Equal(expectedTableFQNs))
				})

				Specify("That the previous timestamp entries should NOT have the current backup set FQNs", func() {
					expectedTableFQNs := []string{"public.ao1", "public.heap1"}

					Expect(restorePlan[0].TableFQNs).To(Not(ContainElement(expectedTableFQNs[0])))
					Expect(restorePlan[0].TableFQNs).To(Not(ContainElement(expectedTableFQNs[1])))

					Expect(restorePlan[1].TableFQNs).To(Not(ContainElement(expectedTableFQNs[0])))
					Expect(restorePlan[1].TableFQNs).To(Not(ContainElement(expectedTableFQNs[1])))
				})

			})

			Context("A table was dropped between the last full/incremental and this incremental", func() {
				allTables := changedTables[0:1] // exclude "heap1"
				excludedTableFQN := "public.heap1"

				restorePlan := backup.PopulateRestorePlan(changedTables[0:1], previousRestorePlan, allTables)

				Specify("That the added entry should NOT have the dropped table FQN", func() {
					Expect(restorePlan[2].TableFQNs).To(Not(ContainElement(excludedTableFQN)))
				})

				Specify("That the previous timestamp entries should NOT have the dropped table FQN", func() {
					Expect(restorePlan[0].TableFQNs).To(Not(ContainElement(excludedTableFQN)))
					Expect(restorePlan[1].TableFQNs).To(Not(ContainElement(excludedTableFQN)))
				})

			})
		})

	})
	Describe("GetLatestMatchingBackupTimestamp", func() {
		var log *Buffer
		BeforeEach(func() {
			_, _, log = testhelper.SetupTestLogger()
		})
		AfterEach(func() {
			operating.InitializeSystemFunctions()
		})
		It("fatals when trying to take an incremental backup without a full backup", func() {
			backup.SetFPInfo(filepath.FilePathInfo{UserSpecifiedBackupDir: "/tmp", UserSpecifiedSegPrefix: "/test-prefix"})
			backup.SetReport(&report.Report{})

			Expect(func() { backup.GetLatestMatchingBackupTimestamp() }).Should(Panic())
			Expect(log.Contents()).To(ContainSubstring("There was no matching previous backup found with the flags provided. Please take a full backup."))

		})
	})
})
