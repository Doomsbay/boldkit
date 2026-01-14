#!/usr/bin/env bash
set -euo pipefail

input_tsv="${1:-BOLD_Public.26-Sep-2025/BOLD_Public.26-Sep-2025.tsv}"
output_tsv="${2:-taxonkit_input.tsv}"

if [[ -s "${output_tsv}" ]]; then
  echo "Output exists, skipping: ${output_tsv}" >&2
  exit 0
fi

if [[ ! -f "${input_tsv}" ]]; then
  echo "Input TSV not found: ${input_tsv}" >&2
  exit 1
fi

awk -F'\t' -v OFS="\t" '
NR==1{
  for(i=1;i<=NF;i++){
    if($i=="processid") pid=i;
    if($i=="bin_uri") bin=i;
    if($i=="kingdom") kingdom=i;
    if($i=="phylum") phylum=i;
    if($i=="class") class=i;
    if($i=="order") ord=i;
    if($i=="family") family=i;
    if($i=="subfamily") subfamily=i;
    if($i=="tribe") tribe=i;
    if($i=="genus") genus=i;
    if($i=="species") species=i;
  }
  if(!pid || !bin || !kingdom || !phylum || !class || !ord || !family || !subfamily || !tribe || !genus || !species){
    print "Required headers missing in input TSV" > "/dev/stderr";
    exit 1;
  }
  print "kingdom","phylum","class","order","family","subfamily","tribe","genus","species","processid";
  next
}
{
  for(i=1;i<=NF;i++) if($i=="None") $i="";

  pidv=$(pid);
  binv=$(bin);
  kingdomv=$(kingdom);
  phylumv=$(phylum);
  classv=$(class);
  ordv=$(ord);
  familyv=$(family);
  subfamilyv=$(subfamily);
  tribev=$(tribe);
  genv=$(genus);
  specv=$(species);

  if(genv!="" && specv==""){
    suffix = (binv!="" ? binv : pidv);
    specv = genv " sp. " suffix;
  }

  print kingdomv,phylumv,classv,ordv,familyv,subfamilyv,tribev,genv,specv,pidv
}
' "${input_tsv}" > "${output_tsv}"
