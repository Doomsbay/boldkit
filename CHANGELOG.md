# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- `-gap-policy keep`: frame-preserving gap handling for BOLD's aligned sequences — terminal gaps become `N`, internal gaps are preserved verbatim, terminal `N` runs are exempt from `-max-n`/`-max-ambig` and reported as `terminal_n`/`padded_records` in the QC report. Available on `qc`, `classify`, and `split`.
- `format -classifier protax` now emits `protax_refs.csv` per library in the PROTAX-SSL schema (`phylum,class,order,family,genus,species,dna_barcode`) with tree-wide homonym-disambiguated, comma-free taxon names. Existing protax FASTA/TSV outputs are unchanged. See the wiki's PROTAX-SSL page for the training workflow.

### Fixed
- Unknown classifier names (including mixed valid/invalid selections) are rejected before any output file is created; the unimplemented `dnasketch` option was removed from help.

## [v0.6.0]

### Added
- `-markers` flag on `markers` and `pipeline` to select a case-insensitive subset of markers (e.g. `COI-5P,MATK`); empty selects all, zero-match selections warn.
- `-gap-policy drop|merge` QC setting for gap handling with report counts; gaps are preserved verbatim through the marker stage and QC alone owns sequence filtering.

### Fixed
- Packaging now rejects existing release outputs without `-force` and preserves input files when publication fails (all-or-nothing with `.bak` restore).
- Per-marker skip when FASTA outputs already exist; zero-match marker warnings are now accurate.
- QC rejects output paths that alias the input FASTA — direct paths, absolute spelling variants, symlinks, and hard links — instead of truncating the input and reporting success.
- QC taxonomy-config failures (missing `taxid.map` / taxdump) no longer erase an existing output file.

### Changed
- Command logic moved from `cmd/` into `internal/` packages: `util`, `io`, `progress`, `taxonomy`, `markers`, `extract`, `qc`, and `format`.
- `markers` is now a faithful projection: every record of a selected marker is written (empty/null sequences become empty FASTA records).

## [v0.5.0]

### Added
- Parquet input support: `extract`, `markers`, and `pipeline` commands accept `.parquet` / `.parq` BOLD data files in addition to TSV.
- Auto-detection of input format by file extension — zero configuration required.
- `RowCount()` uses Parquet file metadata for instant row counts instead of line-scanning.
- Unit tests for Parquet row counting and format detection.
- Shell scripts `01_extract_taxonomy_from_bold.sh` and `03_build_marker_fastas.sh` now detect Parquet input and suggest the Go binary.

### Changed
- `extract` and `markers` refactored to use a unified `ParseRows()` API — both TSV and Parquet paths go through the same column-resolution logic.
- Help text updated from "BOLD TSV input" to "BOLD input file (TSV or Parquet)" across all commands.
- `pipeline` logs the detected input format on start.
- Added `github.com/parquet-go/parquet-go` dependency for Parquet reading.

## [v0.4.3]

### Fixed
- Shell scripts: AWK append mode (`>>`) changed to write mode (`>`) in `03_build_marker_fastas.sh` to prevent data duplication on re-runs.
- Shell scripts: Replaced GNU-specific `find -printf` in `05_generate_checksums.sh` with portable alternative (macOS/BSD compatible).
- Shell scripts: Fixed `repo_root` derivation in `06_generate_manifest.sh` to use `git rev-parse --show-toplevel`.
- Shell scripts: JSON output in `06_generate_manifest.sh` now uses Python `json.dumps` to prevent malformed JSON.
- Shell scripts: `subfamily` and `tribe` columns are now optional in `01_extract_taxonomy_from_bold.sh`.
- Shell scripts: Extended null handling from `None` to also cover `NULL` and `NA` in `01_extract_taxonomy_from_bold.sh`.
- Shell scripts: Directory existence checks in `04_package_reference_db.sh` are now conditional on whether the archive needs rebuilding.
- Shell scripts: Spinner process cleanup in `03_build_marker_fastas.sh` now uses `trap` to prevent orphaned processes on error.
- Shell scripts: Fixed help text in `boldkit.sh` referencing old name `bolddb-taxdump.sh`.
- Go: Manifest JSON in `pipeline.go` now uses `encoding/json` instead of `fmt.Sprintf` string interpolation.
- Go: Added depth guard (max 50) to recursive `disambiguate()` in `rdp_taxonomy.go` to prevent unbounded recursion.
- Go: `fileExists()` now returns `true` for zero-byte files.
- Go: Removed duplicate `openText` closure in `format.go` (consolidated to `openFasta`).
- Go: `sintaxLineage()` now logs a warning when ranks beyond species are silently dropped.
- Go: Removed unused `json` struct tags and dead `normalize()` function.

### Changed
- `.gitignore` updated with wildcard BOLD data patterns and `boldkit.wiki/`.
- Added `.gitattributes` with `* text=auto` and `*.sh text eol=lf` to prevent line-ending drift.

## [v0.4.2]

### Added
- Makefile with `build`, `test`, `lint`, `bench`, `cover`, `clean`, and `install` targets.
- `.golangci.yml` configuration for golangci-lint v2 (errcheck, govet, staticcheck, unused, misspell, gofmt).
- `version`, `--version`, `-v` subcommand with automatic version from git tags via ldflags.
- Version displayed in the usage banner.

### Changed
- CI pipeline: upgraded golangci-lint action v4 to v6, replaced `validate` job with `test` job (`make test`), removed redundant `go vet` step.
- Release archives now contain a binary named `boldkit` (or `boldkit.exe`) instead of `boldkit_<version>_<os>_<arch>`, simplifying install.
- `package.sh` now injects version via ldflags so release binaries report the correct version.
- Local builds output to `dist/boldkit` instead of the project root.

### Fixed
- `gofmt` formatting in `fasta.go`, `format.go`, `progress.go`.

## [v0.4.1]

### Added
- RDP taxonomy support in the `format` command.

### Fixed
- `countLines` now correctly returns 1 for files with no trailing newline.
- Fixed misleading indentation in PROTAX map writer block.

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
