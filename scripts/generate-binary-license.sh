#!/usr/bin/env bash
#
# Licensed to the Apache Software Foundation (ASF) under one or more
# contributor license agreements.  See the NOTICE file distributed with
# this work for additional information regarding copyright ownership.
# The ASF licenses this file to You under the Apache License, Version 2.0
# (the "License"); you may not use this file except in compliance with
# the License.  You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# --------------------------------------------------------------------
# Generate the compliance files for the binary (convenience) packages.
#
# The source release contains none of our Go dependencies: there is no
# vendor/ directory, so LICENSE and NOTICE correctly describe the source
# tree on their own.  The binaries we ship are statically linked and
# therefore physically contain the code of every module in the build
# graph, so the binary packages ship LICENSE-binary and NOTICE-binary
# instead.  They are installed under the plain names LICENSE and NOTICE
# by the `package` target in the Makefile.
#
# The component list is derived from the build graph rather than from
# go.mod, so that it stays honest across dependency bumps:
#
#   - only modules linked into a *shipped* binary are listed, so
#     test-only dependencies (ginkgo, gomega, go-sqlmock, ...) are
#     correctly left out;
#   - the union over every released GOOS/GOARCH is taken, because some
#     modules are platform-gated (mdlayher/socket and prometheus/procfs
#     are linked on Linux but not on macOS).
#
# Usage:
#   scripts/generate-binary-license.sh           # (re)generate in place
#   scripts/generate-binary-license.sh --check   # fail if out of date
# --------------------------------------------------------------------

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

# Build tags of the binaries shipped in the convenience packages. Keep in
# sync with the `package` target in the Makefile.
BINARY_TAGS=(
  gpbackup
  gprestore
  gpbackup_helper
  gpbackup_s3_plugin
  gpbackman
  gpbackup_exporter
)

# Platforms built by `make package-all`.
PLATFORMS=(
  linux/amd64
  linux/arm64
)

CHECK_MODE=0
case "${1:-}" in
  --check) CHECK_MODE=1 ;;
  "")      ;;
  *)       echo "usage: $0 [--check]" >&2; exit 2 ;;
esac

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

if (( CHECK_MODE )); then
  OUT_DIR="${WORK_DIR}/out"
else
  OUT_DIR="${REPO_ROOT}"
fi
mkdir -p "${OUT_DIR}"

LICENSE_BINARY="${OUT_DIR}/LICENSE-binary"
NOTICE_BINARY="${OUT_DIR}/NOTICE-binary"
LICENSES_DIR="${OUT_DIR}/licenses-binary"

# --------------------------------------------------------------------
# Resolve the modules actually linked into the shipped binaries.
# --------------------------------------------------------------------

SELF_MODULE="$(go list -m)"

echo "==> Resolving linked modules (${#BINARY_TAGS[@]} binaries x ${#PLATFORMS[@]} platforms)"

MODULES_RAW="${WORK_DIR}/modules.raw"
: > "${MODULES_RAW}"

for platform in "${PLATFORMS[@]}"; do
  goos="${platform%/*}"
  goarch="${platform#*/}"
  for tag in "${BINARY_TAGS[@]}"; do
    GOOS="${goos}" GOARCH="${goarch}" CGO_ENABLED=1 \
      go list -tags "${tag}" -deps \
        -f '{{if .Module}}{{.Module.Path}}	{{.Module.Version}}	{{.Module.Dir}}{{end}}' . \
      >> "${MODULES_RAW}"
  done
done

MODULES="${WORK_DIR}/modules"
grep -v "^${SELF_MODULE}	" "${MODULES_RAW}" | sort -u > "${MODULES}"

if [[ ! -s "${MODULES}" ]]; then
  echo "ERROR: no modules resolved; is the module cache populated? try 'go mod download'" >&2
  exit 1
fi

echo "==> $(wc -l < "${MODULES}" | tr -d ' ') linked third-party modules"

# --------------------------------------------------------------------
# Classify each module's license.
#
# A module's primary license is the family whose marker appears first in
# its license file: several dependencies (klauspost/compress is the
# clearest case) carry a BSD-licensed body with the Apache and MIT texts
# appended for vendored parts, and matching on "Apache License" alone
# would mislabel those.
# --------------------------------------------------------------------

first_match_line() {
  # first_match_line <file> <pattern> -> line number, or empty
  grep -n -i -m1 -- "$2" "$1" 2>/dev/null | cut -d: -f1
}

classify_license() {
  local lf="$1" best_family="" best_line="" line
  local -a families=(
    "Apache-2.0|Apache License"
    "MIT|Permission is hereby granted, free of charge"
    "BSD|Redistributions of source code must retain"
    "MPL-2.0|Mozilla Public License"
    "ISC|Permission to use, copy, modify, and/or distribute this software"
  )
  local entry family pattern
  for entry in "${families[@]}"; do
    family="${entry%%|*}"
    pattern="${entry#*|}"
    line="$(first_match_line "${lf}" "${pattern}")"
    [[ -z "${line}" ]] && continue
    if [[ -z "${best_line}" || "${line}" -lt "${best_line}" ]]; then
      best_line="${line}"
      best_family="${family}"
    fi
  done
  if [[ "${best_family}" == "BSD" ]]; then
    if grep -q -i -e "Neither the name" -e "name of the author" "${lf}"; then
      best_family="BSD-3-Clause"
    else
      best_family="BSD-2-Clause"
    fi
  fi
  echo "${best_family:-UNKNOWN}"
}

has_multiple_licenses() {
  # true when one license file carries more than one license family
  local lf="$1" count=0 entry pattern
  local -a patterns=(
    "Apache License"
    "Permission is hereby granted, free of charge"
    "Redistributions of source code must retain"
    "Mozilla Public License"
  )
  for pattern in "${patterns[@]}"; do
    if grep -q -i -- "${pattern}" "${lf}" 2>/dev/null; then
      count=$(( count + 1 ))
    fi
  done
  (( count > 1 ))
}

rm -rf "${LICENSES_DIR}"
mkdir -p "${LICENSES_DIR}"

INVENTORY="${WORK_DIR}/inventory"        # family \t module \t version \t relpath \t flags
NOTICES="${WORK_DIR}/notices"            # module \t version \t notice file
: > "${INVENTORY}"
: > "${NOTICES}"

while IFS=$'\t' read -r mod ver dir; do
  [[ -z "${mod}" ]] && continue

  # Kept in a file rather than an array: this script has to run under the
  # bash 3.2 that ships with macOS, which has no mapfile.
  license_list="${WORK_DIR}/license_files"
  find "${dir}" -maxdepth 1 -type f \
    \( -iname 'LICENSE' -o -iname 'LICENSE.*' \
    -o -iname 'LICENCE' -o -iname 'LICENCE.*' \
    -o -iname 'COPYING' -o -iname 'COPYING.*' \) 2>/dev/null | sort > "${license_list}"

  license_count="$(wc -l < "${license_list}" | tr -d ' ')"
  if [[ "${license_count}" -eq 0 ]]; then
    echo "ERROR: no license file found for ${mod} ${ver} (${dir})" >&2
    echo "       inspect the module and record its license by hand" >&2
    exit 1
  fi

  dest_dir="${LICENSES_DIR}/${mod}"
  mkdir -p "${dest_dir}"

  primary=""
  extra_note=""
  while IFS= read -r lf; do
    [[ -z "${lf}" ]] && continue
    base="$(basename "${lf}")"
    cp "${lf}" "${dest_dir}/${base}"
    chmod 0644 "${dest_dir}/${base}"
    if [[ -z "${primary}" ]]; then
      primary="$(classify_license "${lf}")"
      if has_multiple_licenses "${lf}"; then
        extra_note="contains additional license terms for bundled code"
      fi
    fi
  done < "${license_list}"

  if [[ "${license_count}" -gt 1 ]]; then
    extra_note="multiple license files; see licenses/${mod}/"
  fi

  if [[ "${primary}" == "UNKNOWN" ]]; then
    echo "ERROR: could not classify the license of ${mod} ${ver}" >&2
    echo "       inspect ${license_files[0]} and extend classify_license()" >&2
    exit 1
  fi

  # Paths in the generated files name the *installed* location: the
  # licenses-binary/ directory of this repository is installed as
  # licenses/ inside the package, matching apache/cloudberry.
  printf '%s\t%s\t%s\t%s\t%s\n' \
    "${primary}" "${mod}" "${ver}" "licenses/${mod}" "${extra_note}" >> "${INVENTORY}"

  # NOTICE files must be propagated per Apache License 2.0 section 4(d).
  # Match exact names only: lib/pq ships a Go source file called notice.go.
  notice_file="$(find "${dir}" -maxdepth 1 -type f \
    \( -iname 'NOTICE' -o -iname 'NOTICE.txt' -o -iname 'NOTICE.md' \) 2>/dev/null | head -1)"
  if [[ -n "${notice_file}" ]]; then
    printf '%s\t%s\t%s\n' "${mod}" "${ver}" "${notice_file}" >> "${NOTICES}"
  fi
done < "${MODULES}"

# --------------------------------------------------------------------
# Components that are linked in but are not Go modules.
# --------------------------------------------------------------------

GOROOT_DIR="$(go env GOROOT)"
mkdir -p "${LICENSES_DIR}/golang.org/go"
cp "${GOROOT_DIR}/LICENSE" "${LICENSES_DIR}/golang.org/go/LICENSE"
cp "${GOROOT_DIR}/PATENTS" "${LICENSES_DIR}/golang.org/go/PATENTS"
chmod 0644 "${LICENSES_DIR}/golang.org/go/LICENSE" "${LICENSES_DIR}/golang.org/go/PATENTS"

# github.com/mattn/go-sqlite3 embeds the SQLite amalgamation, which is C
# code compiled into every binary built with CGO enabled.
SQLITE_DIR="$(awk -F'\t' '$1 == "github.com/mattn/go-sqlite3" {print $3}' "${MODULES}" | head -1)"
SQLITE_VERSION="unknown"
if [[ -n "${SQLITE_DIR}" && -f "${SQLITE_DIR}/sqlite3-binding.c" ]]; then
  SQLITE_VERSION="$(grep -m1 '^#define SQLITE_VERSION ' "${SQLITE_DIR}/sqlite3-binding.c" \
    | sed 's/.*"\(.*\)".*/\1/')"
fi

mkdir -p "${LICENSES_DIR}/sqlite.org/sqlite"
cat > "${LICENSES_DIR}/sqlite.org/sqlite/PUBLIC-DOMAIN.txt" <<'SQLITE_EOF'
SQLite Is Public Domain

All of the code and documentation in SQLite has been dedicated to the
public domain by the authors. All code authors, and representatives of
the companies they work for, have signed affidavits dedicating their
contributions to the public domain and originals of those signed
affidavits are stored in a firesafe at the main offices of Hwaci. Anyone
is free to copy, modify, publish, use, compile, sell, or distribute the
original SQLite code, either in source code form or as a compiled binary,
for any purpose, commercial or non-commercial, and by any means.

The previous paragraph applies to the deliverable code and documentation
in SQLite - those parts of the SQLite library that you actually bundle
and ship with a larger application. Some scripts used as part of the
build process (for example the "configure" scripts generated by autoconf)
might fall under other open-source licenses. Nothing from these build
scripts ever reaches the final deliverable SQLite library, however, and
so the licenses associated with those scripts should not be a factor in
assessing your rights to copy and use the SQLite library.

See https://www.sqlite.org/copyright.html for details.
SQLITE_EOF
chmod 0644 "${LICENSES_DIR}/sqlite.org/sqlite/PUBLIC-DOMAIN.txt"

# --------------------------------------------------------------------
# Emit LICENSE-binary.
#
# It opens with the source LICENSE verbatim (the Apache License 2.0 plus
# the Greenplum-derived code, which is compiled into the binaries too)
# and appends an inventory of everything that is bundled only in binary
# form.
# --------------------------------------------------------------------

echo "==> Writing $(basename "${LICENSE_BINARY}")"

{
  cat "${REPO_ROOT}/LICENSE"
  cat <<'HEADER_EOF'

================================================================================
                    BINARY DISTRIBUTION - BUNDLED COMPONENTS
================================================================================

The convenience binary packages of Apache Cloudberry Backup (Incubating)
are statically linked, and therefore contain the code of the third-party
components listed below.  None of these components are present in the
source release; this section applies to the binary artifacts only.

The full text of each component's license is reproduced under the
licenses/ directory of the package, laid out by import path.  (In the
source repository that directory is named licenses-binary/; it is
installed as licenses/ alongside this file.)  NOTICE files required by
section 4(d) of the Apache License 2.0 are reproduced in NOTICE-binary,
which is installed as NOTICE.

This file is generated by scripts/generate-binary-license.sh from the
actual build graph.  Do not edit it by hand; re-run the script instead.

HEADER_EOF

  emit_group() {
    local family="$1" title="$2" description="$3"
    local count
    count="$(awk -F'\t' -v f="${family}" '$1 == f' "${INVENTORY}" | wc -l | tr -d ' ')"
    [[ "${count}" -eq 0 ]] && return 0

    printf -- '--------------------------------------------------------------------------------\n'
    printf '%s\n' "${title}"
    printf -- '--------------------------------------------------------------------------------\n\n'
    printf '%s\n\n' "${description}"
    awk -F'\t' -v f="${family}" '$1 == f {
      if ($5 != "")
        printf "  %s %s\n      (%s)\n", $2, $3, $5
      else
        printf "  %s %s\n", $2, $3
    }' "${INVENTORY}"
    printf '\n'
  }

  emit_group "Apache-2.0" "Apache License 2.0" \
"The following components are licensed under the Apache License, Version
2.0, the full text of which appears at the top of this file.  Where a
component ships a NOTICE file, its contents are reproduced in
NOTICE-binary as required by section 4(d)."

  emit_group "MIT" "MIT License" \
"The following components are licensed under the MIT License.  Each
component's copyright notice and license text is reproduced under
licenses/."

  emit_group "BSD-3-Clause" "BSD 3-Clause License" \
"The following components are licensed under the 3-clause BSD License.
Each component's copyright notice and license text is reproduced under
licenses/."

  emit_group "BSD-2-Clause" "BSD 2-Clause License" \
"The following components are licensed under the 2-clause BSD License.
Each component's copyright notice and license text is reproduced under
licenses/."

  emit_group "MPL-2.0" "Mozilla Public License 2.0" \
"The following components are licensed under the Mozilla Public License
2.0.  Each component's license text is reproduced under licenses/."

  emit_group "ISC" "ISC License" \
"The following components are licensed under the ISC License.  Each
component's license text is reproduced under licenses/."

  cat <<EOF_GO
--------------------------------------------------------------------------------
Go Standard Library and Runtime
--------------------------------------------------------------------------------

Every binary statically links the Go runtime and the parts of the Go
standard library that it uses.  These are licensed under the 3-clause BSD
License, with an additional patent grant.  The exact toolchain version is
recorded in each binary and can be read with "go version <binary>".

  golang.org/go
      (licenses/golang.org/go/LICENSE and PATENTS)

--------------------------------------------------------------------------------
SQLite
--------------------------------------------------------------------------------

The binaries are built with CGO enabled, and github.com/mattn/go-sqlite3
embeds the SQLite amalgamation, so the SQLite C code is compiled into
them.  SQLite has been dedicated to the public domain by its authors.

  sqlite.org/sqlite ${SQLITE_VERSION}
      (licenses/sqlite.org/sqlite/PUBLIC-DOMAIN.txt)

EOF_GO
} > "${LICENSE_BINARY}"

# --------------------------------------------------------------------
# Emit NOTICE-binary.
# --------------------------------------------------------------------

echo "==> Writing $(basename "${NOTICE_BINARY}")"

{
  cat "${REPO_ROOT}/NOTICE"
  cat <<'NOTICE_HEADER_EOF'

================================================================================
                     BINARY DISTRIBUTION - BUNDLED NOTICES
================================================================================

The convenience binary packages bundle the third-party components listed
in LICENSE-binary.  The notices below are reproduced from those bundled
components as required by section 4(d) of the Apache License 2.0.  They
apply to the binary artifacts only; none of these components are present
in the source release.

This file is generated by scripts/generate-binary-license.sh.  Do not
edit it by hand; re-run the script instead.
NOTICE_HEADER_EOF

  while IFS=$'\t' read -r mod ver notice_file; do
    [[ -z "${mod}" ]] && continue
    printf '\n'
    printf -- '--------------------------------------------------------------------------------\n'
    printf '%s %s\n' "${mod}" "${ver}"
    printf -- '--------------------------------------------------------------------------------\n\n'
    # Trim trailing blank lines so the spacing stays uniform.
    awk 'NF {p = 1} p' "${notice_file}" | awk '{lines[NR] = $0} END {
      last = NR
      while (last > 0 && lines[last] ~ /^[[:space:]]*$/) last--
      for (i = 1; i <= last; i++) print lines[i]
    }'
    printf '\n'
  done < <(sort -u "${NOTICES}")
} > "${NOTICE_BINARY}"

chmod 0644 "${LICENSE_BINARY}" "${NOTICE_BINARY}"

NOTICE_COUNT="$(sort -u "${NOTICES}" | wc -l | tr -d ' ')"
echo "==> Reproduced ${NOTICE_COUNT} bundled NOTICE files"

# --------------------------------------------------------------------
# Check mode: compare against what is committed.
# --------------------------------------------------------------------

if (( CHECK_MODE )); then
  status=0
  for path in LICENSE-binary NOTICE-binary; do
    if ! diff -u "${REPO_ROOT}/${path}" "${OUT_DIR}/${path}" > "${WORK_DIR}/${path}.diff" 2>&1; then
      echo "ERROR: ${path} is out of date" >&2
      head -50 "${WORK_DIR}/${path}.diff" >&2
      status=1
    fi
  done
  if ! diff -ru "${REPO_ROOT}/licenses-binary" "${OUT_DIR}/licenses-binary" > "${WORK_DIR}/licenses.diff" 2>&1; then
    echo "ERROR: licenses-binary/ is out of date" >&2
    head -50 "${WORK_DIR}/licenses.diff" >&2
    status=1
  fi
  if (( status )); then
    echo >&2
    echo "Run scripts/generate-binary-license.sh and commit the result." >&2
    exit 1
  fi
  echo "==> Binary compliance files are up to date"
fi

echo "==> Done"
