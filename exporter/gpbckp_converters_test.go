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

package exporter

import (
	"github.com/apache/cloudberry-backup/history"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Converters", func() {
	Describe("convertBoolToFloat64", func() {
		It("returns 1 for true", func() {
			Expect(convertBoolToFloat64(true)).To(Equal(float64(1)))
		})
		It("returns 0 for false", func() {
			Expect(convertBoolToFloat64(false)).To(Equal(float64(0)))
		})
	})

	Describe("convertEmptyLabel", func() {
		It("returns 'none' for empty string", func() {
			Expect(convertEmptyLabel("")).To(Equal("none"))
		})
		It("returns original string for non-empty string", func() {
			Expect(convertEmptyLabel("text")).To(Equal("text"))
		})
	})

	Describe("convertStatusFloat64", func() {
		It("returns 1 for Failure status", func() {
			Expect(convertStatusFloat64(history.BackupStatusFailed)).To(Equal(float64(1)))
		})
		It("returns 0 for non-Failure status", func() {
			Expect(convertStatusFloat64("text")).To(Equal(float64(0)))
		})
	})
})
