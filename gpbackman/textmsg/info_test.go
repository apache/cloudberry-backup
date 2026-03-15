package textmsg

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("info tests", func() {
	Describe("info text functions with one arg", func() {
		It("returns correct info text", func() {
			tests := []struct {
				name     string
				value    string
				function func(string) string
				want     string
			}{
				{"InfoTextBackupDeleteStart", "TestBackup", InfoTextBackupDeleteStart, "Start deleting backup TestBackup"},
				{"InfoTextBackupDeleteSuccess", "TestBackup", InfoTextBackupDeleteSuccess, "Backup TestBackup successfully deleted"},
				{"InfoTextBackupAlreadyDeleted", "TestBackup", InfoTextBackupAlreadyDeleted, "Backup TestBackup has already been deleted"},
				{"InfoTextBackupDirPath", "/test/path", InfoTextBackupDirPath, "Path to backup directory: /test/path"},
				{"InfoTextSegmentPrefix", "TestValue", InfoTextSegmentPrefix, "Segment Prefix: TestValue"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.value)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("info text functions with two args", func() {
		It("returns correct info text", func() {
			tests := []struct {
				name     string
				value1   string
				value2   string
				function func(string, string) string
				want     string
			}{
				{"InfoTextBackupStatus", "TestBackup", "In Progress", InfoTextBackupStatus, "Backup TestBackup has status: In Progress"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.value1, tt.value2)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("info text functions with multiple args", func() {
		It("returns correct info text", func() {
			tests := []struct {
				name      string
				value     string
				valueList []string
				function  func(string, []string) string
				want      string
			}{
				{"InfoTextBackupDependenciesList", "TestBackup1", []string{"TestBackup2", "TestBackup3"}, InfoTextBackupDependenciesList, "Backup TestBackup1 has dependent backups: TestBackup2, TestBackup3"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.value, tt.valueList)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("info text functions with multiple separate args", func() {
		It("returns correct info text", func() {
			tests := []struct {
				name     string
				values   []string
				function func(...string) string
				want     string
			}{
				{"InfoTextCommandExecution", []string{"execution_command", "some_argument"}, InfoTextCommandExecution, "Executing command: execution_command some_argument"},
				{"InfoTextCommandExecutionSucceeded", []string{"execution_command", "some_argument"}, InfoTextCommandExecutionSucceeded, "Command succeeded: execution_command some_argument"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.values...)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("info text functions with list args", func() {
		It("returns correct info text", func() {
			tests := []struct {
				name     string
				values   []string
				function func([]string) string
				want     string
			}{
				{"InfoTextBackupDeleteList", []string{"TestBackup1", "TestBackup2"}, InfoTextBackupDeleteList, "The following backups will be deleted: TestBackup1, TestBackup2"},
				{"InfoTextBackupDeleteListFromHistory", []string{"TestBackup1", "TestBackup2"}, InfoTextBackupDeleteListFromHistory, "The following backups will be deleted from history: TestBackup1, TestBackup2"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.values)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("info text functions with no args", func() {
		It("returns correct info text", func() {
			Expect(InfoTextNothingToDo()).To(Equal("Nothing to do"))
		})
	})
})
