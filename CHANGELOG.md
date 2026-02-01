# Changelog

All notable changes to this project will be documented in this file.

## [v0.2.1] - 2026-01-27

### Added
- New `qc` command for FASTA filtering (length, ambiguity, invalid chars, dedupe, required taxonomy ranks).
- New `format` command to generate classifier-ready outputs (BLAST, Kraken2, SINTAX, RDP, IDTAXA, PROTAX).
- New `classify` pipeline that chains QC + format with per-classifier output directories.
- Optional compression of classifier outputs via `-compress`.
- Approximate progress bars for `qc` and `format`.
- Bash wrapper `scripts/07_classifier_pipeline.sh` with COI-5P defaults.

### Documentation
- Wiki updates for QC, format, classify, and the classifier pipeline script.
