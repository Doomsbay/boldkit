#!/usr/bin/env bash
set -euo pipefail

input_tsv="${1:-BOLD_Public.26-Sep-2025/BOLD_Public.26-Sep-2025.tsv}"
output_dir="${2:-marker_fastas}"

if [[ -d "${output_dir}" ]] && compgen -G "${output_dir}/*.fasta" > /dev/null; then
  echo "Marker FASTAs already exist, skipping: ${output_dir}" >&2
  exit 0
fi

if [[ ! -f "${input_tsv}" ]]; then
  echo "Input TSV not found: ${input_tsv}" >&2
  exit 1
fi

mkdir -p "${output_dir}"

awk -F'\t' -v OUTDIR="${output_dir}" '
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
' "${input_tsv}"
