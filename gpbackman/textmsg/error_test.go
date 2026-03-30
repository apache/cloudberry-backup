/*
Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements.  See the NOTICE file
distributed with this work for additional information
regarding copyright ownership.  The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License.  You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied.  See the License for the
specific language governing permissions and limitations
under the License.
*/

package textmsg

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("error tests", func() {
	Describe("error text functions with error only", func() {
		It("returns correct error text", func() {
			testError := errors.New("test error")
			tests := []struct {
				name     string
				function func(error) string
				want     string
			}{
				{"ErrorTextUnableReadHistoryDB", ErrorTextUnableReadHistoryDB, "Unable to read data from history db. Error: test error"},
				{"ErrorTextUnableGetReport", ErrorTextUnableGetReport, "Unable to get report. Error: test error"},
				{"ErrorTextUnableCheckTimestamp", ErrorTextUnableCheckTimestamp, "Unable to check timestamp. Error: test error"},
				{"ErrorTextUnableGetSegPrefix", ErrorTextUnableGetSegPrefix, "Unable to get segment prefix. Error: test error"},
				{"ErrorTextUnableCheckPath", ErrorTextUnableCheckPath, "Unable to check path. Error: test error"},
				{"ErrorTextUnableDeleteLocalBackup", ErrorTextUnableDeleteLocalBackup, "Unable to delete local backup. Error: test error"},
				{"ErrorTextUnableCleanDB", ErrorTextUnableCleanDB, "Unable to clean db. Error: test error"},
			}
			for _, tt := range tests {
				Expect(tt.function(testError)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("error text functions with error and arg", func() {
		It("returns correct error text", func() {
			testError := errors.New("test error")
			tests := []struct {
				name     string
				value    string
				function func(string, error) string
				want     string
			}{
				{"ErrorTextUnableGetBackupInfo", "TestValue", ErrorTextUnableGetBackupInfo, "Unable to get info for backup TestValue. Error: test error"},
				{"ErrorTextUnableDeletePluginBackup", "TestValue", ErrorTextUnableDeletePluginBackup, "Unable to delete plugin backup TestValue. Error: test error"},
				{"ErrorTextUnableDeletePluginReport", "TestValue", ErrorTextUnableDeletePluginReport, "Unable to delete plugin report TestValue. Error: test error"},
				{"ErrorTextUnableUpdateDeleteStatus", "TestValue", ErrorTextUnableUpdateDeleteStatus, "Unable to update delete status for TestValue. Error: test error"},
				{"ErrorTextUnableCheckBackupDir", "TestValue", ErrorTextUnableCheckBackupDir, "Unable to check backup dir TestValue. Error: test error"},
				{"ErrorTextUnableActionHistoryDB", "TestAction", ErrorTextUnableActionHistoryDB, "Unable to TestAction history db. Error: test error"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.value, testError)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("error text functions with error and two args", func() {
		It("returns correct error text", func() {
			testError := errors.New("test error")
			tests := []struct {
				name     string
				value1   string
				value2   string
				function func(string, string, error) string
				want     string
			}{
				{"ErrorTextUnableDeleteFile", "TestValue1", "TestValue2", ErrorTextUnableDeleteFile, "Unable to delete TestValue1 TestValue2. Error: test error"},
			}
			for _, tt := range tests {
				Expect(tt.function(tt.value1, tt.value2, testError)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("error text functions with error and multiple args", func() {
		It("returns correct error text", func() {
			testError := errors.New("test error")
			tests := []struct {
				name     string
				values   []string
				function func(error, ...string) string
				want     string
			}{
				{"ErrorTextUnableCompatibleFlags", []string{"flag1", "flag2"}, ErrorTextUnableCompatibleFlags, "Unable to use the following flags together: flag1, flag2. Error: test error"},
			}
			for _, tt := range tests {
				Expect(tt.function(testError, tt.values...)).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("error functions with one arg", func() {
		It("returns correct error", func() {
			tests := []struct {
				name     string
				value    string
				function func(string) error
				want     string
			}{
				{"ErrorBackupNotFoundError", "TestBackup", ErrorBackupNotFoundError, "backup TestBackup not found"},
				{"ErrorInvalidInputValueError", "TestValue", ErrorInvalidInputValueError, "invalid input value: TestValue"},
			}
			for _, tt := range tests {
				err := tt.function(tt.value)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("error functions with two args", func() {
		It("returns correct error", func() {
			tests := []struct {
				name     string
				value1   string
				value2   string
				function func(string, string) error
				want     string
			}{
				{"ErrorSetBackupDeleteStatus", "TestBackup", "TestStatus", ErrorSetBackupDeleteStatus, "backup TestBackup has delete status TestStatus"},
			}
			for _, tt := range tests {
				err := tt.function(tt.value1, tt.value2)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(tt.want), tt.name)
			}
		})
	})

	Describe("error functions returning error", func() {
		It("returns correct error", func() {
			tests := []struct {
				name     string
				function func() error
				want     string
			}{
				{"ErrorBackupDirNotSpecifiedError", ErrorBackupDirNotSpecifiedError, "backup dir is not specified"},
				{"ErrorBackupDeleteInProgressError", ErrorBackupDeleteInProgressError, "backup deletion in progress"},
			}
			for _, tt := range tests {
				err := tt.function()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(tt.want), tt.name)
			}
		})
	})
})
