package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/apache/cloudberry-backup/gpbackman/gpbckpconfig"
	"github.com/apache/cloudberry-backup/history"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/pflag"
)

var _ = Describe("wrappers tests", func() {
	Describe("getHistoryDBPath", func() {
		It("returns default path when input is empty", func() {
			Expect(getHistoryDBPath("")).To(Equal(historyDBNameConst))
		})

		It("returns input path when not empty", func() {
			Expect(getHistoryDBPath("path/to/" + historyDBNameConst)).To(Equal("path/to/" + historyDBNameConst))
		})
	})

	Describe("formatBackupDuration", func() {
		It("formats duration correctly", func() {
			tests := []struct {
				name  string
				value float64
				want  string
			}{
				{"01:00:00", 3600, "01:00:00"},
				{"01:01:01", 3661, "01:01:01"},
				{"00:00:00", 0, "00:00:00"},
			}
			for _, tt := range tests {
				Expect(formatBackupDuration(tt.value)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("checkCompatibleFlags", func() {
		It("does not return error when no flags changed", func() {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			Expect(checkCompatibleFlags(flags)).To(Succeed())
		})

		It("does not return error when one flag changed", func() {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String("flag1", "", "")
			flags.Set("flag1", "")
			Expect(checkCompatibleFlags(flags, "flag1")).To(Succeed())
		})

		It("returns error when multiple flags changed", func() {
			flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
			flags.String("flag1", "", "")
			flags.String("flag2", "", "")
			flags.Set("flag1", "")
			flags.Set("flag2", "")
			err := checkCompatibleFlags(flags, "flag1", "flag2")
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("checkBackupCanBeUsed", func() {
		It("returns correct result for various backup configurations", func() {
			tests := []struct {
				name            string
				deleteForce     bool
				skipLocalBackup bool
				backupConfig    history.BackupConfig
				want            bool
				wantErr         bool
			}{
				{
					name:            "successful backup with plugin and force, skipLocalBackup true",
					deleteForce:     true,
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status: history.BackupStatusSucceed,
						Plugin: gpbckpconfig.BackupS3Plugin,
					},
					want: true,
				},
				{
					name:            "successful backup with plugin and without force",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status: history.BackupStatusSucceed,
						Plugin: gpbckpconfig.BackupS3Plugin,
					},
					want: true,
				},
				{
					name:            "failed backup with plugin and force",
					deleteForce:     true,
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status: history.BackupStatusFailed,
						Plugin: gpbckpconfig.BackupS3Plugin,
					},
					want: true,
				},
				{
					name:            "failed backup with plugin and without force",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status: history.BackupStatusFailed,
						Plugin: gpbckpconfig.BackupS3Plugin,
					},
					want: true,
				},
				{
					name:        "successful backup without plugin and force",
					deleteForce: true,
					backupConfig: history.BackupConfig{
						Status: history.BackupStatusSucceed,
					},
					want: true,
				},
				{
					name: "successful backup without plugin and without force",
					backupConfig: history.BackupConfig{
						Status: history.BackupStatusSucceed,
					},
					want: true,
				},
				{
					name:            "successful deleted backup with plugin and force",
					deleteForce:     true,
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						Plugin:      gpbckpconfig.BackupS3Plugin,
						DateDeleted: "20240113210000",
					},
					want: true,
				},
				{
					name:            "successful deleted backup with plugin and without force",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						Plugin:      gpbckpconfig.BackupS3Plugin,
						DateDeleted: "20240113210000",
					},
					want: false,
				},
				{
					name:            "invalid backup status with plugin and without force",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status: "some_status",
						Plugin: gpbckpconfig.BackupS3Plugin,
					},
					want: true,
				},
				{
					name:            "successful backup with plugin with deletion in progress and force",
					deleteForce:     true,
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						Plugin:      gpbckpconfig.BackupS3Plugin,
						DateDeleted: gpbckpconfig.DateDeletedInProgress,
					},
					want: true,
				},
				{
					name:            "successful backup with plugin with deletion in progress and without force",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						Plugin:      gpbckpconfig.BackupS3Plugin,
						DateDeleted: gpbckpconfig.DateDeletedInProgress,
					},
					want: false,
				},
				{
					name:            "successful backup with plugin with invalid deletion date and without force",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						Plugin:      gpbckpconfig.BackupS3Plugin,
						DateDeleted: "some date",
					},
					want: true,
				},
				{
					name: "successful backup with plugin with invalid skipLocalBackup variable",
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						Plugin:      gpbckpconfig.BackupS3Plugin,
						DateDeleted: "some date",
					},
					wantErr: true,
				},
				{
					name:            "successful backup without plugin with invalid skipLocalBackup variable",
					skipLocalBackup: true,
					backupConfig: history.BackupConfig{
						Status:      history.BackupStatusSucceed,
						DateDeleted: "some date",
					},
					wantErr: true,
				},
			}
			for _, tt := range tests {
				got, err := checkBackupCanBeUsed(tt.deleteForce, tt.skipLocalBackup, &tt.backupConfig)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
					Expect(got).To(Equal(tt.want), tt.name)
				}
			}
		})
	})

	Describe("checkBackupType", func() {
		It("accepts valid backup type", func() {
			Expect(checkBackupType(gpbckpconfig.BackupTypeFull)).To(Succeed())
		})

		It("rejects invalid backup type", func() {
			Expect(checkBackupType("InvalidType")).To(HaveOccurred())
		})
	})

	Describe("getBackupMasterDir", func() {
		It("returns correct values for various backup dirs", func() {
			tempDir, err := os.MkdirTemp("", "gpbackman-test-")
			Expect(err).ToNot(HaveOccurred())
			defer os.RemoveAll(tempDir)

			tests := []struct {
				name                  string
				testDir               string
				backupDir             string
				backupDataBackupDir   string
				backupDataDBName      string
				wantBackupMasterDir   string
				wantSegPrefix         string
				wantIsSingleBackupDir bool
				wantErr               bool
			}{
				{
					name:                  "backupDir is set and valid",
					testDir:               filepath.Join(tempDir, "segPrefix", "segment-1", "backups"),
					backupDir:             filepath.Join(tempDir, "segPrefix"),
					wantBackupMasterDir:   filepath.Join(tempDir, "segPrefix", "segment-1"),
					wantSegPrefix:         "segment",
					wantIsSingleBackupDir: false,
				},
				{
					name:                  "backupDataBackupDir is set and valid",
					testDir:               filepath.Join(tempDir, "segPrefix2", "segment-1", "backups"),
					backupDataBackupDir:   filepath.Join(tempDir, "segPrefix2"),
					wantBackupMasterDir:   filepath.Join(tempDir, "segPrefix2", "segment-1"),
					wantSegPrefix:         "segment",
					wantIsSingleBackupDir: false,
				},
			}
			for _, tt := range tests {
				err := os.MkdirAll(tt.testDir, 0o755)
				Expect(err).ToNot(HaveOccurred())
				gotBackupMasterDir, gotSegPrefix, gotIsSingleBackupDir, err := getBackupMasterDir(tt.backupDir, tt.backupDataBackupDir, tt.backupDataDBName)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
					Expect(gotBackupMasterDir).To(Equal(tt.wantBackupMasterDir), tt.name)
					Expect(gotSegPrefix).To(Equal(tt.wantSegPrefix), tt.name)
					Expect(gotIsSingleBackupDir).To(Equal(tt.wantIsSingleBackupDir), tt.name)
				}
			}
		})
	})

	Describe("checkSingleBackupDir", func() {
		It("returns backupDir when isSingleBackupDir is true", func() {
			got := checkSingleBackupDir("/path/to/backup", "seg", "1", true)
			Expect(got).To(Equal("/path/to/backup"))
		})

		It("returns composed path when isSingleBackupDir is false", func() {
			got := checkSingleBackupDir("/path/to/backup", "seg", "1", false)
			Expect(got).To(Equal(filepath.Join("/path/to/backup", fmt.Sprintf("%s%s", "seg", "1"))))
		})
	})

	Describe("getBackupSegmentDir", func() {
		It("returns correct segment dir for various inputs", func() {
			tests := []struct {
				name                string
				backupDir           string
				backupDataBackupDir string
				backupDataDir       string
				isSingleBackupDir   bool
				want                string
				wantErr             bool
			}{
				{
					name:              "backupDir is not empty",
					backupDir:         "/path/to/backupDir",
					isSingleBackupDir: true,
					want:              "/path/to/backupDir",
				},
				{
					name:                "backupDataBackupDir is not empty",
					backupDataBackupDir: "/path/to/backupDataBackupDir",
					isSingleBackupDir:   true,
					want:                "/path/to/backupDataBackupDir",
				},
				{
					name:              "backupDataDir is not empty",
					backupDataDir:     "/path/to/backupDataDir",
					isSingleBackupDir: true,
					want:              "/path/to/backupDataDir",
				},
				{
					name:              "all backup directories are empty",
					isSingleBackupDir: true,
					wantErr:           true,
				},
			}
			for _, tt := range tests {
				got, err := getBackupSegmentDir(tt.backupDir, tt.backupDataBackupDir, tt.backupDataDir, "seg", "1", tt.isSingleBackupDir)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
					Expect(got).To(Equal(tt.want), tt.name)
				}
			}
		})
	})

	Describe("checkLocalBackupStatus", func() {
		It("returns correct result for various inputs", func() {
			tests := []struct {
				name            string
				skipLocalBackup bool
				isLocalBackup   bool
				wantErr         bool
			}{
				{"skip local and local backup", true, true, true},
				{"skip local and plugin backup", true, false, false},
				{"do not skip local and local backup", false, true, false},
				{"do not skip local and plugin backup", false, false, true},
			}
			for _, tt := range tests {
				err := checkLocalBackupStatus(tt.skipLocalBackup, tt.isLocalBackup)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
				}
			}
		})
	})
})
