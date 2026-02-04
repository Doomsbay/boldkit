# Changelog

All notable changes to this project will be documented in this file.

## [v0.4.0]

### Added
- Optional extraction curation profile: `bioscan-5m` (`extract` and `pipeline` pass-through).
- BIOSCAN extraction engine with placeholder normalization, genus/species consistency fixes, subfamily hole filling, BIN-aware canonical species reuse, and deterministic BIN conflict handling.
- Optional extraction curation trace outputs: JSON summary report and per-record audit TSV.
- Unit tests for BIOSCAN species parsing/resolution, conflict policy behavior, protocol fallback behavior, and report/audit generation.
- BIOSCAN BIN canonical species transfer applies only when a BIN has a single resolved species or a strict majority winner; tie/no-majority BINs are treated as conflicted.
- In `bioscan-5m` mode, provisional species fallback does not use `PROCESSID`; provisional labels require BIN.
- Usage details and examples are documented in wiki `Usage` and `Splits-and-BIOSCAN-5M`.

### Documentation
- README kept concise with a short BIOSCAN curation note and link to detailed workflow docs.
- Added new wiki page: `Splits-and-BIOSCAN-5M` (end-to-end BIOSCAN extraction curation + split workflow details).
- Updated wiki Home/Usage links to include the new BIOSCAN + split workflow page.

## [v0.3.0] - 2026-02-02

### Added
- New `split` command for open/closed-world library generation.
- End-to-end split workflow: optional QC, deterministic split assignment, taxdump pruning from `seen_train`, and classifier library formatting.
- New split outputs per marker: `seen_train/val/test`, `test_unseen/val_unseen/keys_unseen`, `other_heldout`, `pretrain`.
- New `split_report.json` with class/record counts and pruned taxid summary.

### Documentation
- Wiki `Usage` expanded with full `split` command documentation and a dedicated full pipeline + split workflow section.
- Added PROTAX status note in docs (currently under review while evaluating PROTAX-GPU and reference intake/build requirements).

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
