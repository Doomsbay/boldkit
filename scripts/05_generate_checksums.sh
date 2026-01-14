#!/usr/bin/env bash
set -euo pipefail

artifact_dir="${1:-artifacts}"
output_file="${2:-${artifact_dir}/SHA256SUMS.txt}"

if [[ ! -d "${artifact_dir}" ]]; then
  echo "Directory not found: ${artifact_dir}" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  hasher=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
  hasher=(shasum -a 256)
else
  echo "sha256sum or shasum not found in PATH" >&2
  exit 1
fi

if [[ "${output_file}" != /* ]]; then
  output_file="$(pwd)/${output_file}"
fi

if [[ -s "${output_file}" ]]; then
  echo "Checksum file exists, skipping: ${output_file}" >&2
  exit 0
fi

mapfile -t files < <(find "${artifact_dir}" -maxdepth 1 -type f -name '*.zip' -printf '%f\n' | sort)

if (( ${#files[@]} == 0 )); then
  echo "No .zip files found in ${artifact_dir}" >&2
  exit 1
fi

(
  cd "${artifact_dir}"
  "${hasher[@]}" "${files[@]}" > "${output_file}"
)
