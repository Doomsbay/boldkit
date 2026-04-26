#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

input_target="${1:-${root_dir}/marker_fastas/COI-5P.fasta.gz}"
output_dir="${2:-${root_dir}/classifier_outputs}"
taxdump_dir="${3:-${root_dir}/bold-taxdump}"
classifiers="${4:-blast}"
markers="${5:-${MARKERS:-COI-5P}}"

compress_output="${COMPRESS_OUTPUT:-false}"

boldkit_bin="${BOLDKIT_BIN:-${root_dir}/boldkit/boldkit}"
if [[ ! -x "${boldkit_bin}" ]]; then
  echo "boldkit binary not found or not executable: ${boldkit_bin}" >&2
  exit 1
fi

compress_flag=""
if [[ "${compress_output}" == "true" ]]; then
  compress_flag="-compress"
fi

if [[ -d "${input_target}" ]]; then
  "${boldkit_bin}" classify \
    -marker-dir "${input_target}" \
    -markers "${markers}" \
    -outdir "${output_dir}" \
    -taxdump-dir "${taxdump_dir}" \
    -classifier "${classifiers}" \
  ${compress_flag}

  exit 0
fi

if [[ ! -f "${input_target}" ]]; then
  echo "Input FASTA not found: ${input_target}" >&2
  exit 1
fi

"${boldkit_bin}" classify \
  -input "${input_target}" \
  -outdir "${output_dir}" \
  -taxdump-dir "${taxdump_dir}" \
  -classifier "${classifiers}" \
  ${compress_flag}
