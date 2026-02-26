#!/bin/bash

# ------------------------------------------------------------------------------
#
# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements. See the NOTICE file distributed
# with this work for additional information regarding copyright
# ownership. The ASF licenses this file to You under the Apache
# License, Version 2.0 (the "License"); you may not use this file
# except in compliance with the License. You may obtain a copy of the
# License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied. See the License for the
# specific language governing permissions and limitations
# under the License.
#
# ------------------------------------------------------------------------------
# Non-perf scale tests for GitHub Actions Cloudberry demo cluster.
# This focuses on backup/restore correctness under moderate object/data scale.
# ------------------------------------------------------------------------------

set -euo pipefail

BACKUP_DIR="${BACKUP_DIR:-/tmp/scale_backup}"
LOG_DIR="${LOG_DIR:-/tmp/scale-test-logs}"

mkdir -p "${BACKUP_DIR}" "${LOG_DIR}"

extract_timestamp() {
  local log_file="$1"
  local ts
  ts="$(grep -E "Backup Timestamp[[:space:]]*=" "${log_file}" | grep -Eo "[[:digit:]]{14}" | head -n 1 || true)"
  if [ -z "${ts}" ]; then
    local latest_gpbackup_log
    latest_gpbackup_log="$(ls -1t "${HOME}/gpAdminLogs"/gpbackup_*.log 2>/dev/null | head -n 1 || true)"
    if [ -n "${latest_gpbackup_log}" ]; then
      ts="$(grep -E "Backup Timestamp[[:space:]]*=" "${latest_gpbackup_log}" | grep -Eo "[[:digit:]]{14}" | head -n 1 || true)"
    fi
  fi
  if [ -z "${ts}" ]; then
    echo "Could not parse backup timestamp from ${log_file}"
    return 1
  fi
  echo "${ts}"
}

validate_datascaledb_restore() {
  local restore_db="$1"
  local src_tables
  local dst_tables
  local src_big
  local dst_big

  src_tables="$(psql -X -d datascaledb -Atc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'")"
  dst_tables="$(psql -X -d "${restore_db}" -Atc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'")"
  src_big="$(psql -X -d datascaledb -Atc "SELECT count(*) FROM tbl_big")"
  dst_big="$(psql -X -d "${restore_db}" -Atc "SELECT count(*) FROM tbl_big")"

  if [ "${src_tables}" != "${dst_tables}" ] || [ "${src_big}" != "${dst_big}" ]; then
    echo "Data scale restore validation failed for ${restore_db}"
    echo "source tables=${src_tables}, restored tables=${dst_tables}"
    echo "source tbl_big=${src_big}, restored tbl_big=${dst_big}"
    return 1
  fi
}

echo "## Preparing copy queue scale database ##"
psql -X -d postgres -qc "DROP DATABASE IF EXISTS copyqueuedb"
createdb copyqueuedb
for j in $(seq 1 300); do
  psql -X -d copyqueuedb -q -c "CREATE TABLE tbl_1k_${j}(i int) DISTRIBUTED BY (i);"
  psql -X -d copyqueuedb -q -c "INSERT INTO tbl_1k_${j} SELECT generate_series(1,1000)"
done

echo "## Copy queue backup/restore matrix ##"
for q in 2 4 8; do
  b_log="${LOG_DIR}/copyqueue_backup_q${q}.log"
  echo "Running gpbackup copy queue size ${q}"
  gpbackup --dbname copyqueuedb --backup-dir "${BACKUP_DIR}" --single-data-file --no-compression --copy-queue-size "${q}" \
    2>&1 | tee "${b_log}"
  timestamp="$(extract_timestamp "${b_log}")"
  restore_db="copyqueue_restore_q${q}"
  psql -X -d postgres -qc "DROP DATABASE IF EXISTS ${restore_db}"
  gprestore --timestamp "${timestamp}" --backup-dir "${BACKUP_DIR}" --create-db --redirect-db "${restore_db}" --copy-queue-size "${q}" \
    2>&1 | tee "${LOG_DIR}/copyqueue_restore_q${q}.log"
  src_tbl_count="$(psql -X -d copyqueuedb -Atc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'")"
  dst_tbl_count="$(psql -X -d "${restore_db}" -Atc "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r'")"
  if [ "${src_tbl_count}" != "${dst_tbl_count}" ]; then
    echo "Copy queue restore validation failed for ${restore_db}: source tables=${src_tbl_count}, restored tables=${dst_tbl_count}"
    exit 1
  fi
done

echo "## Preparing data scale database ##"
psql -X -d postgres -qc "DROP DATABASE IF EXISTS datascaledb"
createdb datascaledb
for j in $(seq 1 200); do
  psql -X -d datascaledb -q -c "CREATE TABLE tbl_1k_${j}(i int) DISTRIBUTED BY (i);"
  psql -X -d datascaledb -q -c "INSERT INTO tbl_1k_${j} SELECT generate_series(1,1000)"
done

psql -X -d datascaledb -q -c "CREATE TABLE tbl_big(i int) DISTRIBUTED BY (i);"
for j in $(seq 1 25); do
  psql -X -d datascaledb -q -c "INSERT INTO tbl_big SELECT generate_series(1,100000)"
done

psql -X -d datascaledb -q -c "CREATE TABLE big_partition(a int, b int, c int) DISTRIBUTED BY (a) PARTITION BY RANGE (b) (START (1) END (101) EVERY (1))"
psql -X -d datascaledb -q -c "INSERT INTO big_partition SELECT i, i, i FROM generate_series(1,100) i"
for j in $(seq 1 8); do
  psql -X -d datascaledb -q -c "INSERT INTO big_partition SELECT * FROM big_partition"
done

echo "## Running data scale backup/restore matrix ##"
run_data_scale_case() {
  local case_name="$1"
  local backup_flags="$2"
  local restore_db="$3"
  local jobs="$4"
  local b_log="${LOG_DIR}/datascale_${case_name}_backup.log"
  local r_log="${LOG_DIR}/datascale_${case_name}_restore.log"

  gpbackup --dbname datascaledb --backup-dir "${BACKUP_DIR}" ${backup_flags} 2>&1 | tee "${b_log}"
  local ts
  ts="$(extract_timestamp "${b_log}")"
  psql -X -d postgres -qc "DROP DATABASE IF EXISTS ${restore_db}"
  gprestore --timestamp "${ts}" --backup-dir "${BACKUP_DIR}" --create-db --redirect-db "${restore_db}" --jobs "${jobs}" \
    2>&1 | tee "${r_log}"
  validate_datascaledb_restore "${restore_db}"
}

run_data_scale_case "multi_data_file" "--leaf-partition-data" "datascale_restore_multi" "4"
run_data_scale_case "multi_data_file_zstd" "--leaf-partition-data --compression-type zstd" "datascale_restore_multi_zstd" "4"
run_data_scale_case "single_data_file" "--leaf-partition-data --single-data-file" "datascale_restore_single" "1"
run_data_scale_case "single_data_file_zstd" "--leaf-partition-data --single-data-file --compression-type zstd" "datascale_restore_single_zstd" "1"

echo "## Preparing metadata scale database ##"
psql -X -d postgres -qc "DROP DATABASE IF EXISTS metadatascaledb"
createdb metadatascaledb

psql -X -d metadatascaledb <<'SQL'
DO $$
DECLARE
  i int;
BEGIN
  FOR i IN 1..80 LOOP
    EXECUTE format('CREATE SCHEMA IF NOT EXISTS s_%s', i);
    EXECUTE format('CREATE TABLE s_%s.t_%s(id int, val text) DISTRIBUTED BY (id)', i, i);
    EXECUTE format('CREATE VIEW s_%s.v_%s AS SELECT * FROM s_%s.t_%s', i, i, i, i);
  END LOOP;
END$$;
SQL

echo "## Running metadata-only backup/restore ##"
meta_backup_log="${LOG_DIR}/metadata_backup.log"
meta_restore_log="${LOG_DIR}/metadata_restore.log"
gpbackup --dbname metadatascaledb --backup-dir "${BACKUP_DIR}" --metadata-only --verbose 2>&1 | tee "${meta_backup_log}"
meta_ts="$(extract_timestamp "${meta_backup_log}")"
psql -X -d postgres -qc "DROP DATABASE IF EXISTS metadatascaledb_res"
gprestore --timestamp "${meta_ts}" --backup-dir "${BACKUP_DIR}" --redirect-db metadatascaledb_res --jobs 4 --create-db \
  2>&1 | tee "${meta_restore_log}"

echo "## Minimal correctness checks ##"
src_schema_count="$(psql -X -d metadatascaledb -Atc "SELECT count(*) FROM pg_namespace WHERE nspname LIKE 's_%'")"
dst_schema_count="$(psql -X -d metadatascaledb_res -Atc "SELECT count(*) FROM pg_namespace WHERE nspname LIKE 's_%'")"
if [ "${src_schema_count}" != "${dst_schema_count}" ]; then
  echo "Metadata restore schema count mismatch: src=${src_schema_count} dst=${dst_schema_count}"
  exit 1
fi

echo "Scale tests completed successfully"
