all: build

ifndef GOPATH
$(error Environment variable GOPATH is not set)
endif

.DEFAULT_GOAL := all
BACKUP=gpbackup
RESTORE=gprestore
HELPER=gpbackup_helper
S3PLUGIN=gpbackup_s3_plugin
BIN_DIR=$(shell echo $${GOPATH:-~/go} | awk -F':' '{ print $$1 "/bin"}')
GINKGO_FLAGS := -r --keep-going --randomize-suites --randomize-all --no-color
GIT_VERSION := $(shell v=$$(git describe --tags 2>/dev/null); if [ -n "$$v" ]; then echo $$v | perl -pe 's/(.*)-([0-9]*)-(g[0-9a-f]*)/\1+dev.\2.\3/'; else cat VERSION 2>/dev/null || echo "dev"; fi)
BACKUP_VERSION_STR=github.com/apache/cloudberry-backup/backup.version=$(GIT_VERSION)
RESTORE_VERSION_STR=github.com/apache/cloudberry-backup/restore.version=$(GIT_VERSION)
HELPER_VERSION_STR=github.com/apache/cloudberry-backup/helper.version=$(GIT_VERSION)
S3PLUGIN_VERSION_STR=github.com/apache/cloudberry-backup/plugins/s3plugin.version=$(GIT_VERSION)

# note that /testutils is not a production directory, but has unit tests to validate testing tools
SUBDIRS_HAS_UNIT=backup/ filepath/ history/ helper/ options/ report/ restore/ toc/ utils/ testutils/ plugins/s3plugin/
SUBDIRS_ALL=$(SUBDIRS_HAS_UNIT) integration/ end_to_end/
GOLANG_LINTER=$(GOPATH)/bin/golangci-lint
GINKGO=$(GOPATH)/bin/ginkgo
GOIMPORTS=$(GOPATH)/bin/goimports
GO_BUILD=go build -mod=readonly
DEBUG=-gcflags=all="-N -l"

CUSTOM_BACKUP_DIR ?= "/tmp"
helper_path ?= $(BIN_DIR)/$(HELPER)
s3plugin_path ?= $(BIN_DIR)/$(S3PLUGIN)

# Prefer gpsync as the newer utility, fall back to gpscp if not present (older installs)
ifeq (, $(shell which gpsync))
COPYUTIL=gpscp
else
COPYUTIL=gpsync
endif

depend :
	go mod download

$(GINKGO) :
	go install github.com/onsi/ginkgo/v2/ginkgo

$(GOIMPORTS) :
	go install golang.org/x/tools/cmd/goimports@latest

$(GOSQLITE) :
	go install github.com/mattn/go-sqlite3

format : $(GOIMPORTS)
		@goimports -w $(shell find . -type f -name '*.go' -not -path "./vendor/*")

LINTER_VERSION=1.16.0
$(GOLANG_LINTER) :
		mkdir -p $(GOPATH)/bin
		curl -sfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v${LINTER_VERSION}

.PHONY : coverage integration end_to_end

lint : $(GOLANG_LINTER)
		golangci-lint run --tests=false

unit : $(GINKGO)
	TEST_DB_TYPE=CBDB TEST_DB_VERSION=2.999.0 ginkgo $(GINKGO_FLAGS) $(SUBDIRS_HAS_UNIT) 2>&1

unit_all_gpdb_versions : $(GINKGO)
	TEST_DB_TYPE=CBDB TEST_DB_VERSION=2.999.0 ginkgo $(GINKGO_FLAGS) $(SUBDIRS_HAS_UNIT) 2>&1
	TEST_DB_TYPE=GPDB TEST_DB_VERSION=5.999.0 ginkgo $(GINKGO_FLAGS) $(SUBDIRS_HAS_UNIT) 2>&1
	TEST_DB_TYPE=GPDB TEST_DB_VERSION=6.999.0 ginkgo $(GINKGO_FLAGS) $(SUBDIRS_HAS_UNIT) 2>&1
	TEST_DB_TYPE=GPDB TEST_DB_VERSION=7.999.0 ginkgo $(GINKGO_FLAGS) $(SUBDIRS_HAS_UNIT) 2>&1 # GPDB main

integration : $(GINKGO)
	ginkgo $(GINKGO_FLAGS) integration 2>&1

test : build unit integration

end_to_end : $(GINKGO)
	ginkgo $(GINKGO_FLAGS) --timeout=3h --poll-progress-after=0s end_to_end -- --custom_backup_dir $(CUSTOM_BACKUP_DIR) 2>&1

coverage :
		@./show_coverage.sh

build : $(GOSQLITE)
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(BACKUP)' -o $(BIN_DIR)/$(BACKUP) --ldflags '-X $(BACKUP_VERSION_STR)'
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(RESTORE)' -o $(BIN_DIR)/$(RESTORE) --ldflags '-X $(RESTORE_VERSION_STR)'
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(HELPER)' -o $(BIN_DIR)/$(HELPER) --ldflags '-X $(HELPER_VERSION_STR)'
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(S3PLUGIN)' -o $(BIN_DIR)/$(S3PLUGIN) --ldflags '-X $(S3PLUGIN_VERSION_STR)'

debug :
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(BACKUP)' -o $(BIN_DIR)/$(BACKUP) -ldflags "-X $(BACKUP_VERSION_STR)" $(DEBUG)
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(RESTORE)' -o $(BIN_DIR)/$(RESTORE) -ldflags "-X $(RESTORE_VERSION_STR)" $(DEBUG)
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(HELPER)' -o $(BIN_DIR)/$(HELPER) -ldflags "-X $(HELPER_VERSION_STR)" $(DEBUG)
		CGO_ENABLED=1 $(GO_BUILD) -tags '$(S3PLUGIN)' -o $(BIN_DIR)/$(S3PLUGIN) -ldflags "-X $(S3PLUGIN_VERSION_STR)" $(DEBUG)

build_linux :
		env GOOS=linux GOARCH=amd64 $(GO_BUILD) -tags '$(BACKUP)' -o $(BACKUP) -ldflags "-X $(BACKUP_VERSION_STR)"
		env GOOS=linux GOARCH=amd64 $(GO_BUILD) -tags '$(RESTORE)' -o $(RESTORE) -ldflags "-X $(RESTORE_VERSION_STR)"
		env GOOS=linux GOARCH=amd64 $(GO_BUILD) -tags '$(HELPER)' -o $(HELPER) -ldflags "-X $(HELPER_VERSION_STR)"
		env GOOS=linux GOARCH=amd64 $(GO_BUILD) -tags '$(S3PLUGIN)' -o $(S3PLUGIN) -ldflags "-X $(S3PLUGIN_VERSION_STR)"

install :
		cp $(BIN_DIR)/$(BACKUP) $(BIN_DIR)/$(RESTORE) $(GPHOME)/bin
		@psql -X -t -d template1 -c 'select distinct hostname from gp_segment_configuration where content != -1' > /tmp/seg_hosts 2>/dev/null; \
		if [ $$? -eq 0 ]; then \
			$(COPYUTIL) -f /tmp/seg_hosts $(helper_path) $(s3plugin_path) =:$(GPHOME)/bin/; \
			if [ $$? -eq 0 ]; then \
				echo 'Successfully copied gpbackup_helper and gpbackup_s3_plugin to $(GPHOME) on all segments'; \
			else \
				echo 'Failed to copy gpbackup_helper and gpbackup_s3_plugin to $(GPHOME)'; \
				exit 1;	 \
			fi; \
		else \
			echo 'Database is not running, please start the database and run this make target again'; \
				exit 1;	 \
		fi; \
		rm /tmp/seg_hosts

clean :
		# Build artifacts
		rm -f $(BIN_DIR)/$(BACKUP) $(BACKUP) $(BIN_DIR)/$(RESTORE) $(RESTORE) $(BIN_DIR)/$(HELPER) $(HELPER) $(BIN_DIR)/$(S3PLUGIN) $(S3PLUGIN)
		# Test artifacts
		rm -rf /tmp/go-build* /tmp/gexec_artifacts* /tmp/ginkgo*
		docker stop s3-minio # stop minio before removing its data directories
		docker rm s3-minio
		rm -rf /tmp/minio
		rm -f /tmp/minio_config.yaml
		# Code coverage files
		rm -rf /tmp/cover* /tmp/unit*
		go clean -i -r -x -testcache -modcache

error-report:
	@echo "Error messaging:"
	@echo ""
	@ag "gplog.Error|gplog.Fatal|ors.New|errors.Error|CheckClusterError|GpexpandFailureMessage =|errMsg :=" --ignore "*_test*" | grep -v "FatalOnError(err)" | grep -v ".Error()"

warning-report:
	@echo "Warning messaging:"
	@echo ""
	@ag "gplog.Warn" --ignore "*_test*"

info-report:
	@echo "Info and verbose messaging:"
	@echo ""
	@ag "gplog.Info|gplog.Verbose" --ignore "*_test*"

test-s3-local: build install
	${PWD}/plugins/generate_minio_config.sh
	mkdir -p /tmp/minio/gpbackup-s3-test
	docker run -d --name s3-minio --memory="2g" -p 9000:9000 -p 9001:9001 -v /tmp/minio:/data/minio quay.io/minio/minio server /data/minio --console-address ":9001"
	sleep 2 # Wait for minio server to start up
	${PWD}/plugins/plugin_test.sh $(BIN_DIR)/gpbackup_s3_plugin /tmp/minio_config.yaml
	docker stop s3-minio
	docker rm s3-minio

# Packaging targets
# NOTE: Build on a baseline system with older glibc (e.g., Rocky 8)
# for maximum runtime compatibility across distributions.
PACKAGE_NAME=apache-cloudberry-backup-incubating
PACKAGE_VERSION=$(shell cat VERSION 2>/dev/null || git describe --tags --exact-match 2>/dev/null || git describe --tags --always | sed 's/-.*//' || echo "dev")

# Auto-detect current platform if not specified
CURRENT_GOOS=$(shell go env GOOS)
CURRENT_GOARCH=$(shell go env GOARCH)
GOOS ?= $(CURRENT_GOOS)
GOARCH ?= $(CURRENT_GOARCH)

BUILD_DIR=build

# CGO is required for SQLite support
CGO ?= 1

package:
	@echo "Building package for $(GOOS)/$(GOARCH) with CGO_ENABLED=$(CGO)..."
	@mkdir -p $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/bin
	@GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) go build -tags '$(BACKUP)' -o $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/bin/$(BACKUP) --ldflags '-X $(BACKUP_VERSION_STR)'
	@GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) go build -tags '$(RESTORE)' -o $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/bin/$(RESTORE) --ldflags '-X $(RESTORE_VERSION_STR)'
	@GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) go build -tags '$(HELPER)' -o $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/bin/$(HELPER) --ldflags '-X $(HELPER_VERSION_STR)'
	@GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO) go build -tags '$(S3PLUGIN)' -o $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/bin/$(S3PLUGIN) --ldflags '-X $(S3PLUGIN_VERSION_STR)'
	@echo "Creating install script..."
	@echo '#!/bin/bash' > $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'set -e' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '# Use GPHOME if set, otherwise use default path' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'if [ -n "$$GPHOME" ]; then' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '    INSTALL_DIR="$$GPHOME"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'elif [ -n "$$INSTALL_DIR" ]; then' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '    INSTALL_DIR="$$INSTALL_DIR"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'else' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '    INSTALL_DIR="/usr/local"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'fi' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'SCRIPT_DIR="$$(cd "$$(dirname "$${BASH_SOURCE[0]}")" && pwd)"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'echo "Installing $(PACKAGE_NAME) to $$INSTALL_DIR..."' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '# Install binary files' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'sudo cp "$${SCRIPT_DIR}/bin/"* "$${INSTALL_DIR}/bin/"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '# Set permissions' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'sudo chmod 755 "$${INSTALL_DIR}/bin/$(BACKUP)"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'sudo chmod 755 "$${INSTALL_DIR}/bin/$(RESTORE)"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'sudo chmod 755 "$${INSTALL_DIR}/bin/$(HELPER)"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'sudo chmod 755 "$${INSTALL_DIR}/bin/$(S3PLUGIN)"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo '' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'echo "Installation complete!"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo 'echo "$(PACKAGE_NAME) binaries installed to $${INSTALL_DIR}/bin/"' >> $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@chmod +x $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/install.sh
	@echo "Creating tar.gz package..."
	@cd $(BUILD_DIR) && tar -czf $(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH).tar.gz $(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH)/
	@echo "Package created: $(BUILD_DIR)/$(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH).tar.gz"
	@echo "Contents:"
	@cd $(BUILD_DIR) && tar -tzf $(PACKAGE_NAME)-$(PACKAGE_VERSION)-$(GOOS)-$(GOARCH).tar.gz | head -20

package-linux-amd64:
	@echo "Building Linux AMD64 package..."
	GOOS=linux GOARCH=amd64 make package

package-linux-arm64:
	@echo "Building Linux ARM64 package..."
	GOOS=linux GOARCH=arm64 make package

package-all: package-linux-amd64 package-linux-arm64
	@echo "All packages built successfully!"

package-clean:
	@rm -rf $(BUILD_DIR)
	@echo "Build directory cleaned"

.PHONY: package package-linux-amd64 package-linux-arm64 package-all package-clean
