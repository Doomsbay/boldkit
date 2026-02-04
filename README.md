# BoldKit
[![CI](https://github.com/Doomsbay/boldkit/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/Doomsbay/boldkit/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Doomsbay/boldkit)](https://github.com/Doomsbay/boldkit/releases)
[![Downloads](https://img.shields.io/github/downloads/Doomsbay/boldkit/total)](https://github.com/Doomsbay/boldkit/releases)
[![DOI](https://zenodo.org/badge/DOI/10.5281/zenodo.18382196.svg)](https://doi.org/10.5281/zenodo.18382196)
[![License](https://img.shields.io/github/license/Doomsbay/boldkit)](LICENSE)

Build NCBI-style taxonomy databases and marker-specific FASTA reference sets from BOLD public snapshots.

## Overview
- Build `taxonkit_input.tsv`, `nodes.dmp`, `names.dmp`, `taxid.map`, and per-marker FASTAs from BOLD snapshots.
- Run classifier preparation workflows (`qc`, `format`, `classify`, `split`) for benchmark-ready reference libraries.
- Package outputs into release artifacts with checksums and manifest metadata.
- For full command/flag details, use the wiki ([Usage](https://github.com/Doomsbay/boldkit/wiki/Usage)).

## Requirements
- Bash, awk (gawk)
- TaxonKit (>= 0.16) for taxdump generation
- zip, gzip
- sha256sum or shasum (for checksums)
- Optional: BoldKit Go (download/compile) for faster extraction/markers; see wiki for details.

## Quick start (bash pipeline)
```bash
# Use your snapshot path
bash bolddb-taxdump.sh --input ./BOLD_Public.*/BOLD_Public.*.tsv

# Add packaging (zips, manifest, checksums)
bash bolddb-taxdump.sh \
  --input ./BOLD_Public.*/BOLD_Public.*.tsv \
  --package

# Add BoldKit binaries to the release folder
bash bolddb-taxdump.sh \
  --input ./BOLD_Public.*/BOLD_Public.*.tsv \
  --package \
  --package-binaries
```
Note: BOLD snapshots are very large; processing can take a long time.

## BoldKit CLI (Go)
If you prefer the Go tools for speed, download or build the BoldKit binary and see the GitHub wiki for commands and parameters. The bash pipeline does not require Go.

Wiki pages:
- [Download](https://github.com/Doomsbay/boldkit/wiki/Download)
- [Quick Start](https://github.com/Doomsbay/boldkit/wiki/Quick-Start)
- [Usage](https://github.com/Doomsbay/boldkit/wiki/Usage)
- [Splits + BIOSCAN-5M](https://github.com/Doomsbay/boldkit/wiki/Splits-and-BIOSCAN-5M)
- [Examples](https://github.com/Doomsbay/boldkit/wiki/Examples)
- [FAQ](https://github.com/Doomsbay/boldkit/wiki/FAQ)

## Pipeline workflow
`bolddb-taxdump.sh` runs:
1) Extract taxonomy to `taxonkit_input.tsv`.
2) Build taxdump using taxonkit(`nodes.dmp`, `names.dmp`, `taxid.map`).
3) Build marker FASTAs (`marker_fastas/*.fasta.gz`).
4) Package zip archives (when `--package` is set).
5) Generate `releases/manifest.json` (unless `--skip-manifest`).
6) Generate `releases/SHA256SUMS.txt` (unless `--skip-checksums`).

Manual equivalents live in `scripts/01`–`06`.

## Open/closed-world library workflow
Use `boldkit split` to run QC, open/closed-world partitioning, taxdump pruning, and classifier library formatting.

```bash
./bin/boldkit split \
  -marker-dir marker_fastas \
  -markers COI-5P \
  -outdir libraries
```

See wiki [Usage](https://github.com/Doomsbay/boldkit/wiki/Usage) for full split flags, output layout, and workflow variants.

## Output graph
```
BOLD_Public.*.tsv
  -> taxonkit_input.tsv
  -> bold-taxdump/
       nodes.dmp
       names.dmp
       taxid.map
  -> marker_fastas/
       COI-5P.fasta.gz
       ITS.fasta.gz
       ...

releases/
  bold-taxdump.<snapshot>.tar.gz
  marker_fastas.<snapshot>.tar.gz
  taxonkit_input.<snapshot>.tsv.gz
  manifest.json
  SHA256SUMS.txt
```
When `--package` is set in the Go pipeline, the generated folders are moved under `releases/` before compression and removed after packaging completes.

## How taxonomy is constructed
Ranks: `kingdom -> phylum -> class -> order -> family -> subfamily -> tribe -> genus -> species`
- Missing intermediate ranks are skipped.
- If genus exists but species is missing, species is filled as `Genus sp. BIN` (or `Genus sp. PROCESSID`).
- Optional extraction curation (`bioscan-5m`) is available; see wiki for rules and processing details.
- Records without genus attach to the deepest available rank.
- No artificial “Unidentified” taxa are created.
- Each sequence is attached as a leaf using its processid.

## Working with multiple BOLD releases
Run the pipeline on any snapshot (e.g., `BOLD_Public.2023-xx`, `BOLD_Public.2024-xx`, `BOLD_Public.2025-xx`). Each snapshot yields its own taxdump, marker FASTAs, and release artifacts for longitudinal comparisons.

## Data policy and releases
- Large generated data are not committed to Git; they are published under GitHub Releases as `bold-taxdump.<snapshot>.tar.gz`, `marker_fastas.<snapshot>.tar.gz`, `taxonkit_input.<snapshot>.tsv.gz`, `manifest.json`, and `SHA256SUMS.txt`.
- Each release should record the BOLD snapshot ID and the pipeline commit hash for reproducibility.

## Data citation
Users are strongly encouraged to cite data retrieved from BOLD and the tools
used in this pipeline.

Ratnasingham, Sujeevan, and Paul D N Hebert. "BOLD: The Barcode of Life Data System (http://www.barcodinglife.org)." Molecular Ecology Notes 7(3) (2007): 355-364. doi:10.1111/j.1471-8286.2007.01678.x

BOLD snapshot DOI: https://doi.org/10.5883/DP-BOLD_Public.26-Sep-2025

TaxonKit paper: https://doi.org/10.1016/j.jgg.2021.03.006

BoldKit Zenodo DOI: https://doi.org/10.5281/zenodo.18382196

## License
The source code in this repository is licensed under the MIT License.
