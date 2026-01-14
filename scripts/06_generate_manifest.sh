#!/usr/bin/env bash
set -euo pipefail

artifact_dir="${1:-releases}"
taxdump_dir="${2:-bolddb-taxdump}"
marker_dir="${3:-marker_fastas}"
snapshot_id="${4:-}"
output_file="${5:-${artifact_dir}/manifest.json}"

if [[ -z "${snapshot_id}" ]]; then
  echo "Snapshot ID is required" >&2
  exit 1
fi

if [[ ! -d "${artifact_dir}" ]]; then
  echo "Directory not found: ${artifact_dir}" >&2
  exit 1
fi

if [[ -s "${output_file}" ]]; then
  echo "Manifest exists, skipping: ${output_file}" >&2
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

commit_hash="unknown"
repo_root="$(cd "$(dirname "${artifact_dir}")" && pwd)"
if command -v git >/dev/null 2>&1; then
  if git -C "${repo_root}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if git -C "${repo_root}" rev-parse --verify HEAD >/dev/null 2>&1; then
      commit_hash="$(git -C "${repo_root}" rev-parse HEAD)"
    fi
  fi
fi

nodes_count="$(wc -l < "${taxdump_dir}/nodes.dmp" | tr -d ' ')"
names_count="$(wc -l < "${taxdump_dir}/names.dmp" | tr -d ' ')"
taxid_count="$(wc -l < "${taxdump_dir}/taxid.map" | tr -d ' ')"

marker_seq_count="0"
# Updated to find .fasta.gz files
mapfile -d '' marker_files < <(find "${marker_dir}" -maxdepth 1 -type f -name '*.fasta.gz' -print0)
marker_file_count="${#marker_files[@]}"

if (( marker_file_count > 0 )); then
  # Use zcat to stream content and grep to count headers
  # xargs ensures we don't hit command line length limits with too many files
  marker_seq_count="$(find "${marker_dir}" -maxdepth 1 -name '*.fasta.gz' -print0 | xargs -0 zcat | grep -c '^>')"
fi

cat <<EOF > "${output_file}"
{
  "snapshot_id": "${snapshot_id}",
  "commit_hash": "${commit_hash}",
  "counts": {
    "nodes": ${nodes_count},
    "names": ${names_count},
    "taxid_map": ${taxid_count},
    "marker_fasta_files": ${marker_file_count},
    "marker_fasta_sequences": ${marker_seq_count}
  }
}
EOF
