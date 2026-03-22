package gpbckpconfig

import (
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("utils tests", func() {
	Describe("CheckTimestamp", func() {
		It("accepts valid timestamp", func() {
			Expect(CheckTimestamp("20230822120000")).To(Succeed())
		})

		It("rejects invalid timestamp", func() {
			Expect(CheckTimestamp("invalid")).To(HaveOccurred())
		})

		It("rejects wrong length timestamp", func() {
			Expect(CheckTimestamp("2023082212000")).To(HaveOccurred())
		})
	})

	Describe("CheckFullPath", func() {
		It("accepts existing file with full path", func() {
			tempFile, err := os.CreateTemp("", "testfile")
			Expect(err).ToNot(HaveOccurred())
			defer os.Remove(tempFile.Name())
			Expect(CheckFullPath(tempFile.Name(), true)).To(Succeed())
		})

		It("rejects non-existing file with full path", func() {
			Expect(CheckFullPath("/some/path/test.txt", true)).To(HaveOccurred())
		})

		It("rejects empty path", func() {
			Expect(CheckFullPath("", false)).To(HaveOccurred())
		})

		It("rejects relative path", func() {
			Expect(CheckFullPath("test.txt", false)).To(HaveOccurred())
		})
	})

	Describe("IsBackupActive", func() {
		It("returns correct result for various date deleted values", func() {
			tests := []struct {
				name  string
				value string
				want  bool
			}{
				{"empty delete date", "", true},
				{"plugin error", DateDeletedPluginFailed, true},
				{"local error", DateDeletedLocalFailed, true},
				{"deletion in progress", DateDeletedInProgress, false},
				{"deleted", "20220401102430", false},
			}
			for _, tt := range tests {
				Expect(IsBackupActive(tt.value)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("IsPositiveValue", func() {
		It("returns true for positive value", func() {
			Expect(IsPositiveValue(10)).To(BeTrue())
		})

		It("returns false for zero", func() {
			Expect(IsPositiveValue(0)).To(BeFalse())
		})

		It("returns false for negative value", func() {
			Expect(IsPositiveValue(-5)).To(BeFalse())
		})
	})

	Describe("backupS3PluginReportPath", func() {
		It("returns correct path for valid options", func() {
			got, err := backupS3PluginReportPath("20230112131415", map[string]string{"folder": "/path/to/folder"})
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal("/path/to/folder/backups/20230112/20230112131415/gpbackup_20230112131415_report"))
		})

		It("returns error for missing options", func() {
			_, err := backupS3PluginReportPath("20230112131415", nil)
			Expect(err).To(HaveOccurred())
		})

		It("returns error for wrong options key", func() {
			_, err := backupS3PluginReportPath("20230112131415", map[string]string{"wrong_key": "/path/to/folder"})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ReportFileName", func() {
		It("returns correct report file name", func() {
			Expect(ReportFileName("202301011234")).To(Equal("gpbackup_202301011234_report"))
		})
	})

	Describe("backupPluginCustomReportPath", func() {
		It("returns correct path", func() {
			tests := []struct {
				name      string
				timestamp string
				folder    string
				want      string
			}{
				{"basic", "20230101123456", "/backup/folder", "/backup/folder/gpbackup_20230101123456_report"},
				{"trailing slashes", "20230101123456", "/backup//folder//", "/backup/folder/gpbackup_20230101123456_report"},
				{"folder with spaces", "20230101123456", "/backup/folder with spaces", "/backup/folder with spaces/gpbackup_20230101123456_report"},
				{"no leading slash", "20230101123456", "backup/folder with spaces", "/backup/folder with spaces/gpbackup_20230101123456_report"},
			}
			for _, tt := range tests {
				Expect(backupPluginCustomReportPath(tt.timestamp, tt.folder)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("GetTimestampOlderThan", func() {
		It("returns timestamp within expected range", func() {
			input := uint(1)
			got := GetTimestampOlderThan(input)
			parsedTime, err := time.ParseInLocation(Layout, got, time.Now().Location())
			Expect(err).ToNot(HaveOccurred())
			now := time.Now()
			expected := now.AddDate(0, 0, -int(input))
			Expect(parsedTime.Before(now)).To(BeTrue())
			Expect(parsedTime.Sub(expected).Seconds()).To(BeNumerically("<=", 1))
		})
	})

	Describe("CheckTableFQN", func() {
		It("accepts valid table name", func() {
			Expect(CheckTableFQN("public.table_1")).To(Succeed())
		})

		It("rejects invalid table name", func() {
			Expect(CheckTableFQN("invalid_table")).To(HaveOccurred())
		})
	})

	Describe("ReportFilePath", func() {
		It("returns correct report file path", func() {
			got := ReportFilePath("/path/to/backup", "20230101123456")
			Expect(got).To(Equal("/path/to/backup/backups/20230101/20230101123456/gpbackup_20230101123456_report"))
		})
	})

	Describe("GetSegPrefix", func() {
		It("returns correct segment prefix", func() {
			Expect(GetSegPrefix("/path/to/backup/segment-1/backups")).To(Equal("segment"))
		})
	})

	Describe("CheckMasterBackupDir", func() {
		It("returns correct values for various backup dirs", func() {
			tempDir := os.TempDir()
			tests := []struct {
				name                string
				testDir             string
				backupDir           string
				wantDir             string
				wantPrefix          string
				wantSingleBackupDir bool
				wantErr             bool
			}{
				{
					name:                "valid single backup dir",
					testDir:             filepath.Join(tempDir, "noSegPrefix", "backups"),
					backupDir:           filepath.Join(tempDir, "noSegPrefix"),
					wantDir:             filepath.Join(tempDir, "noSegPrefix"),
					wantPrefix:          "",
					wantSingleBackupDir: true,
				},
				{
					name:                "valid backup dir with segment prefix",
					testDir:             filepath.Join(tempDir, "segPrefix", "segment-1", "backups"),
					backupDir:           filepath.Join(tempDir, "segPrefix"),
					wantDir:             filepath.Join(tempDir, "segPrefix", "segment-1"),
					wantPrefix:          "segment",
					wantSingleBackupDir: false,
				},
				{
					name:      "invalid backup dir",
					testDir:   filepath.Join(tempDir, "invalid"),
					backupDir: filepath.Join(tempDir, "invalid"),
					wantErr:   true,
				},
				{
					name:      "non-existent path",
					testDir:   tempDir,
					backupDir: "some/path",
					wantErr:   true,
				},
			}
			for _, tt := range tests {
				err := os.MkdirAll(tt.testDir, 0o755)
				Expect(err).ToNot(HaveOccurred())
				defer os.Remove(tt.testDir)
				gotDir, gotPrefix, gotIsSingleBackupDir, err := CheckMasterBackupDir(tt.backupDir)
				if tt.wantErr {
					Expect(err).To(HaveOccurred(), tt.name)
				} else {
					Expect(err).ToNot(HaveOccurred(), tt.name)
					Expect(gotDir).To(Equal(tt.wantDir), tt.name)
					Expect(gotPrefix).To(Equal(tt.wantPrefix), tt.name)
					Expect(gotIsSingleBackupDir).To(Equal(tt.wantSingleBackupDir), tt.name)
				}
			}
		})
	})

	Describe("BackupDirPath", func() {
		It("returns correct backup dir path", func() {
			tests := []struct {
				name      string
				backupDir string
				timestamp string
				want      string
			}{
				{"basic path", "/data/backup", "20230101123456", "/data/backup/backups/20230101/20230101123456"},
				{"path with trailing slash", "/data/backup/", "20230101123456", "/data/backup/backups/20230101/20230101123456"},
			}
			for _, tt := range tests {
				Expect(BackupDirPath(tt.backupDir, tt.timestamp)).To(Equal(tt.want), tt.name)
			}
		})
	})


})
