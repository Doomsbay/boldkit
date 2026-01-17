#!/usr/bin/env bash
set -euo pipefail

input_tsv="${1:-./BOLD_Public.*/BOLD_Public.*.tsv}"
output_dir="${2:-marker_fastas}"

# Skip if outputs already exist unless FORCE=true
if [[ -d "${output_dir}" ]] && (compgen -G "${output_dir}/*.fasta" > /dev/null || compgen -G "${output_dir}/*.fasta.gz" > /dev/null); then
  if [[ "${FORCE:-false}" != "true" ]]; then
    echo "Marker FASTAs already exist, skipping: ${output_dir}" >&2
    exit 0
  fi
fi

if [[ "${input_tsv}" == *"*"* ]]; then
  mapfile -t matches < <(compgen -G "${input_tsv}")
  if (( ${#matches[@]} == 0 )); then
    echo "Input TSV not found: ${input_tsv}" >&2
    exit 1
  fi
  if (( ${#matches[@]} > 1 )); then
    echo "Input TSV glob matched multiple files; pass explicit path." >&2
    printf '  %s\n' "${matches[@]}" >&2
    exit 1
  fi
  input_tsv="${matches[0]}"
fi

if [[ ! -f "${input_tsv}" ]]; then
  echo "Input TSV not found: ${input_tsv}" >&2
  exit 1
fi

mkdir -p "${output_dir}"

spinner_pid=""
start_time="$(date +%s)"
spinner_chars="|/-\\"
spinner() {
  local i=0
  while true; do
    local now elapsed
    now="$(date +%s)"
    elapsed=$((now - start_time))
    printf "\r%c elapsed %02d:%02d" "${spinner_chars:i%4:1}" "$((elapsed/60))" "$((elapsed%60))" >&2
    i=$((i+1))
    sleep 0.2
  done
}

echo "Parsing BOLD snapshot and writing FASTA files..." >&2
spinner &
spinner_pid=$!

awk -F'\t' -v OUTDIR="${output_dir}" -f - "${input_tsv}" <<'AWK'
BEGIN{OFS="\n"}
NR==1{
  for(i=1;i<=NF;i++){
    if($i=="processid") pid=i;
    if($i=="marker_code") mk=i;
    if($i=="nuc") nuc=i;
  }
  if(!pid || !mk || !nuc){
    print "Required headers missing in input TSV" > "/dev/stderr";
    exit 1;
  }
  next
}
{
  if($(nuc)=="" || $(nuc)=="None") next;

  m=$(mk);
  if(m=="" || m=="None") m="UNKNOWN";

  gsub(/[^A-Za-z0-9._-]+/,"_",m);

  seq=$(nuc);
  gsub(/[^ACGTacgt]/,"",seq);
  if(length(seq)<1) next;

  out=OUTDIR "/" m ".fasta";
  print ">"$(pid), toupper(seq) >> out;

}
AWK

if [[ -n "${spinner_pid}" ]]; then
  kill "${spinner_pid}" >/dev/null 2>&1 || true
  wait "${spinner_pid}" 2>/dev/null || true
  printf "\rDone. elapsed %02d:%02d\n" "$(( ( $(date +%s) - start_time ) / 60 ))" "$(( ( $(date +%s) - start_time ) % 60 ))" >&2
fi

if [[ "${GZIP_OUTPUT:-true}" == "true" ]]; then
  if ! command -v gzip >/dev/null 2>&1; then
    echo "gzip not found in PATH" >&2
    exit 1
  fi

  shopt -s nullglob
  fasta_files=("${output_dir}"/*.fasta)
  echo "Compressing ${#fasta_files[@]} FASTA files..."
  if (( ${#fasta_files[@]} == 0 )); then
    echo "No FASTA files found to compress in ${output_dir}" >&2
    exit 0
  fi
  gzip "${fasta_files[@]}"
fi
