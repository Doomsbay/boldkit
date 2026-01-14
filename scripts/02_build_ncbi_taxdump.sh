#!/usr/bin/env bash
set -euo pipefail

input_tsv="${1:-taxonkit_input.tsv}"
output_dir="${2:-bolddb-taxdump}"
accession_col="${3:-10}"

if [[ -s "${output_dir}/nodes.dmp" && -s "${output_dir}/names.dmp" && -s "${output_dir}/taxid.map" ]]; then
  echo "Taxdump already exists, skipping: ${output_dir}" >&2
  exit 0
fi

if [[ ! -f "${input_tsv}" ]]; then
  echo "Input TSV not found: ${input_tsv}" >&2
  exit 1
fi

taxonkit_bin="${TAXONKIT_BIN:-}"
if [[ -z "${taxonkit_bin}" ]]; then
  if command -v taxonkit >/dev/null 2>&1; then
    taxonkit_bin="taxonkit"
  elif command -v taxonkit.exe >/dev/null 2>&1; then
    taxonkit_bin="taxonkit.exe"
  else
    echo "taxonkit not found in PATH" >&2
    exit 1
  fi
fi

input_arg="${input_tsv}"
output_arg="${output_dir}"
if [[ "${taxonkit_bin}" == *.exe ]]; then
  if ! command -v wslpath >/dev/null 2>&1; then
    echo "wslpath not found; cannot convert paths for taxonkit.exe" >&2
    exit 1
  fi
  mkdir -p "${output_dir}"
  input_abs="$(cd "$(dirname "${input_tsv}")" && pwd)/$(basename "${input_tsv}")"
  output_abs="$(cd "${output_dir}" && pwd)"
  input_arg="$(wslpath -w "${input_abs}")"
  output_arg="$(wslpath -w "${output_abs}")"
fi

"${taxonkit_bin}" create-taxdump "${input_arg}" \
  -A "${accession_col}" \
  --null None,NULL,NA \
  -O "${output_arg}" \
  --force
