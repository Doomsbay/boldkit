#!/usr/bin/env bash
set -euo pipefail

output_dir="${1:-artifacts}"
taxdump_dir="${2:-bolddb-taxdump}"
marker_dir="${3:-marker_fastas}"
version_tag="${4:-}"

taxdump_name="$(basename "${taxdump_dir}")"
marker_name="$(basename "${marker_dir}")"

suffix=""
if [[ -n "${version_tag}" ]]; then
  safe_tag="$(printf '%s' "${version_tag}" | tr -c 'A-Za-z0-9._-' '_')"
  suffix=".${safe_tag}"
fi

taxdump_zip="${output_dir}/${taxdump_name}${suffix}.zip"
marker_zip="${output_dir}/${marker_name}${suffix}.zip"

need_taxdump="true"
need_marker="true"
if [[ -s "${taxdump_zip}" ]]; then
  need_taxdump="false"
fi
if [[ -s "${marker_zip}" ]]; then
  need_marker="false"
fi

if [[ "${need_taxdump}" == "false" && "${need_marker}" == "false" ]]; then
  echo "Zip artifacts already exist, skipping: ${output_dir}" >&2
  exit 0
fi

if [[ ! -d "${taxdump_dir}" ]]; then
  echo "Directory not found: ${taxdump_dir}" >&2
  exit 1
fi

if [[ ! -d "${marker_dir}" ]]; then
  echo "Directory not found: ${marker_dir}" >&2
  exit 1
fi

mkdir -p "${output_dir}"

if ! command -v zip >/dev/null 2>&1; then
  echo "zip not found in PATH" >&2
  exit 1
fi

if [[ "${need_taxdump}" == "true" ]]; then
  zip -r "${taxdump_zip}" "${taxdump_dir}"
fi
if [[ "${need_marker}" == "true" ]]; then
  zip -r "${marker_zip}" "${marker_dir}"
fi
