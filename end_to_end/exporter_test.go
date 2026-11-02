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

package end_to_end_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getFreeTCPPort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

type exporterMetricCheck struct {
	pattern string
	count   int
}

func validateExporterMetrics(body string, checks []exporterMetricCheck) {
	lines := strings.Split(body, "\n")
	for _, em := range checks {
		re := regexp.MustCompile(em.pattern)
		count := 0
		for _, line := range lines {
			if re.MatchString(line) {
				count++
			}
		}
		Expect(count).To(Equal(em.count),
			fmt.Sprintf("metric pattern %q: got %d, want %d", em.pattern, count, em.count))
	}
}

func fetchMetrics(url string, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return string(body), fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return string(body), nil
}

var _ = Describe("gpbackup_exporter end to end tests", func() {
	BeforeEach(func() {
		if useOldBackupVersion {
			Skip("exporter tests are not applicable in old backup version mode")
		}
		if _, err := os.Stat(exporterPath); os.IsNotExist(err) {
			Skip("gpbackup_exporter binary not found, skipping exporter e2e tests")
		}
	})

	It("collects and exposes metrics without web config", func() {
		expectedMetrics := []exporterMetricCheck{
			{`^gpbackup_backup_deletion_status\{`, 3},
			{`^gpbackup_backup_duration_seconds\{`, 3},
			{`^gpbackup_backup_info\{`, 3},
			{`^gpbackup_backup_status\{`, 3},
			{`^gpbackup_backup_status\{.*\} 0$`, 3},
			{`^gpbackup_backup_duration_seconds\{.*object_filtering="none",plugin="none".*\}`, 3},
			{`^gpbackup_backup_info\{.*database_name="testdb".*\}`, 3},
			{`^gpbackup_backup_since_last_completion_seconds\{`, 3},
			{`^gpbackup_backup_since_last_completion_seconds\{.*backup_type="full",database_name="testdb".*\}`, 1},
			{`^gpbackup_backup_since_last_completion_seconds\{.*backup_type="incremental",database_name="testdb".*\}`, 1},
			{`^gpbackup_backup_since_last_completion_seconds\{.*backup_type="metadata-only",database_name="testdb".*\}`, 1},
			{`^gpbackup_exporter_status\{database_name="testdb"\} 1$`, 1},
			{`^gpbackup_exporter_build_info\{.*\} 1$`, 1},
		}
		end_to_end_setup()
		defer end_to_end_teardown()
		historyDB := getHistoryDBPathForCluster()
		gpbackup(gpbackupPath, backupHelperPath,
			"--backup-dir", backupDir,
			"--leaf-partition-data")
		gpbackup(gpbackupPath, backupHelperPath,
			"--backup-dir", backupDir,
			"--incremental",
			"--leaf-partition-data")
		gpbackup(gpbackupPath, backupHelperPath,
			"--backup-dir", backupDir,
			"--metadata-only")
		port := getFreeTCPPort()
		cmd := exec.Command(exporterPath,
			"--gpbackup.history-file", historyDB,
			"--collect.interval", "600",
			"--web.listen-address", fmt.Sprintf("127.0.0.1:%d", port),
		)
		Expect(cmd.Start()).To(Succeed())
		defer cmd.Process.Kill()
		metricsURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
		var body string
		Eventually(func() string {
			b, err := fetchMetrics(metricsURL, nil)
			if err != nil {
				return ""
			}
			body = b
			return b
		}, 10*time.Second, 500*time.Millisecond).Should(ContainSubstring("gpbackup_backup_status"))
		validateExporterMetrics(body, expectedMetrics)
	})
})
