#!/usr/bin/env bash
set -euo pipefail

output_dir="${1:-releases}"
taxdump_dir="${2:-bold-taxdump}"
marker_dir="${3:-marker_fastas}"
version_tag="${4:-}"

# Create output directory immediately so we can resolve its absolute path
mkdir -p "${output_dir}"
# Resolve output_dir to absolute path because we will change directories later
output_dir="$(cd "${output_dir}" && pwd)"

taxdump_name="$(basename "${taxdump_dir}")"
marker_name="$(basename "${marker_dir}")"

suffix=""
if [[ -n "${version_tag}" ]]; then
  safe_tag="$(printf '%s' "${version_tag}" | tr -c 'A-Za-z0-9._-' '_')"
  suffix=".${safe_tag}"
fi

taxdump_archive="${output_dir}/${taxdump_name}${suffix}.tar.gz"
marker_archive="${output_dir}/${marker_name}${suffix}.tar.gz"

need_taxdump="true"
need_marker="true"

if [[ -s "${taxdump_archive}" ]]; then
  need_taxdump="false"
fi
if [[ -s "${marker_archive}" ]]; then
  need_marker="false"
fi

if [[ "${need_taxdump}" == "false" && "${need_marker}" == "false" ]]; then
  echo "Release packages already exist, skipping: ${output_dir}" >&2
  exit 0
fi

if [[ "${need_taxdump}" == "true" && ! -d "${taxdump_dir}" ]]; then
  echo "Directory not found: ${taxdump_dir}" >&2
  exit 1
fi

if [[ "${need_marker}" == "true" && ! -d "${marker_dir}" ]]; then
  echo "Directory not found: ${marker_dir}" >&2
  exit 1
fi

if ! command -v tar >/dev/null 2>&1; then
  echo "tar not found in PATH" >&2
  exit 1
fi

# Function to safely gzip a directory from its parent (tar.gz)
# usage: tar_dir "path/to/target_dir" "absolute/path/to/output.tar.gz"
tar_dir() {
  local target="$1"
  local output="$2"
  local parent
  local name

  parent="$(cd "$(dirname "${target}")" && pwd)"
  name="$(basename "${target}")"

  echo "Archiving ${name} into ${output}..."
  (
    cd "${parent}"
    tar -czf "${output}" "${name}"
  )
}

if [[ "${need_taxdump}" == "true" ]]; then
  tar_dir "${taxdump_dir}" "${taxdump_archive}"
fi

if [[ "${need_marker}" == "true" ]]; then
  tar_dir "${marker_dir}" "${marker_archive}"
fi
