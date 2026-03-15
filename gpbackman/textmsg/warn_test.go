package textmsg

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("warn tests", func() {
	Describe("warn text functions with one arg", func() {
		It("returns correct warn text", func() {
			tests := []struct {
				name     string
				value    string
				function func(string) string
				want     string
			}{
				{"WarnTextBackupUnableGetReport", "TestBackup", WarnTextBackupUnableGetReport, "Unable to get report for backup TestBackup. Check if backup is active"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.value)).To(Equal(tt.want), tt.name)
			}
		})
	})
})
