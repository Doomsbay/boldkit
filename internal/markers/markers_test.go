package markers

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/klauspost/pgzip"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
)

func TestBuildWarningsUseOriginalMarkerCodes(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	content := "processid\tmarker_code\tnuc\nP1\tCOI/5P\tACGT\nP2\tCOI:5P\tTGCA\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "stderr.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	stderr := os.Stderr
	os.Stderr = logFile
	t.Cleanup(func() {
		os.Stderr = stderr
		_ = logFile.Close()
	})
	selected := ParseSelection("coi/5p,COI:5P,COI_5P,MATK")
	if err := BuildMarkerFastas(input, outDir, false, 0, -1, 2, selected); err != nil {
		t.Fatalf("BuildMarkerFastas: %v", err)
	}
	assertMarkerFasta(t, outDir, "COI_5P", false, ">P1\nACGT\n>P2\nTGCA\n")
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "[boldkit] markers: warning: selected marker \"COI_5P\" matched 0 records\n" +
		"[boldkit] markers: warning: selected marker \"MATK\" matched 0 records\n" +
		"[boldkit] markers: wrote 2 records across 1 marker(s)\n"
	if string(logs) != want {
		t.Fatalf("unexpected marker diagnostics:\ngot: %s\nwant: %s", logs, want)
	}
}

func TestBuildMissingSelection(t *testing.T) {
	for _, gzipOut := range []bool{false, true} {
		for _, selection := range []string{"MATK", "coi-5p,matk"} {
			t.Run(selection+"/gzip="+strconv.FormatBool(gzipOut), func(t *testing.T) {
				dir := t.TempDir()
				input := filepath.Join(dir, "input.tsv")
				outDir := filepath.Join(dir, "markers")
				writeMarkerFixture(t, input, "ACGT", "TGCA")
				selected, skip, err := SelectMissing(outDir, ParseSelection("COI-5P"), false)
				if err != nil {
					t.Fatal(err)
				}
				if skip {
					t.Fatal("expected COI-5P to be missing")
				}
				if err := BuildMarkerFastas(input, outDir, gzipOut, 0, -1, 2, selected); err != nil {
					t.Fatalf("BuildMarkerFastas: %v", err)
				}
				writeMarkerFixture(t, input, "AAAA", "CCCC")
				selected, skip, err = SelectMissing(outDir, ParseSelection(selection), false)
				if err != nil {
					t.Fatal(err)
				}
				if skip {
					t.Fatal("expected missing markers in selection")
				}
				if err := BuildMarkerFastas(input, outDir, !gzipOut, 0, -1, 2, selected); err != nil {
					t.Fatalf("BuildMarkerFastas: %v", err)
				}
				assertMarkerFasta(t, outDir, "COI-5P", gzipOut, ">P1\nACGT\n")
				assertMarkerFasta(t, outDir, "MATK", !gzipOut, ">P2\nCCCC\n")
				entries, err := os.ReadDir(outDir)
				if err != nil {
					t.Fatal(err)
				}
				if len(entries) != 2 {
					t.Fatalf("expected two marker files, got %d", len(entries))
				}
				if _, skip, err := SelectMissing(outDir, ParseSelection("coi-5p,matk"), false); err != nil || !skip {
					t.Fatalf("expected directory-wide skip, got skip=%v err=%v", skip, err)
				}
				selected, skip, err = SelectMissing(outDir, ParseSelection("coi-5p"), true)
				if err != nil || skip {
					t.Fatalf("expected force rebuild, got skip=%v err=%v", skip, err)
				}
				if err := BuildMarkerFastas(input, outDir, gzipOut, 0, -1, 2, selected); err != nil {
					t.Fatalf("BuildMarkerFastas: %v", err)
				}
				assertMarkerFasta(t, outDir, "COI-5P", gzipOut, ">P1\nAAAA\n")
				assertMarkerFasta(t, outDir, "MATK", !gzipOut, ">P2\nCCCC\n")
			})
		}
	}
}

func TestBuildErrorLeavesNoPublishedOutputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "processid\tmarker_code\nP1\tCOI-5P\n"
	if err := os.WriteFile(input, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := BuildMarkerFastas(input, outDir, true, 0, -1, 2, nil); err == nil {
		t.Fatal("expected error for missing headers")
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no outputs after failure, got %v", entryNames(entries))
	}
}

func TestBuildPublishesOnlyAfterSuccess(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outDir, "COI-5P.fasta.gz"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMarkerFixture(t, input, "ACGT", "TGCA")
	err := BuildMarkerFastas(input, outDir, true, 0, -1, 2, ParseSelection("COI-5P"))
	if err == nil {
		t.Fatal("expected publish error")
	}
	if !strings.Contains(err.Error(), "publish marker COI-5P") {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "COI-5P.fasta.gz" || !entries[0].IsDir() {
		t.Fatalf("expected only the pre-existing directory, got %v", entryNames(entries))
	}
}

func TestBuildPublishIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	if err := os.MkdirAll(filepath.Join(outDir, "MATK.fasta.gz"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeMarkerFixture(t, input, "ACGT", "TGCA")
	err := BuildMarkerFastas(input, outDir, true, 0, -1, 2, ParseSelection("COI-5P,MATK"))
	if err == nil {
		t.Fatal("expected publish error")
	}
	if !strings.Contains(err.Error(), "MATK") {
		t.Fatalf("unexpected error: %v", err)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "MATK.fasta.gz" || !entries[0].IsDir() {
		t.Fatalf("expected only the blocking directory, got %v", entryNames(entries))
	}
}

func TestPublishRollsBackPreviousOutputs(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "COI-5P.fasta")
	secondPath := filepath.Join(dir, "MATK.fasta")
	sentinel := "previous COI-5P output"
	if err := os.WriteFile(firstPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(secondPath, 0o755); err != nil {
		t.Fatal(err)
	}
	w1 := newTestWriter(t, firstPath, ">P1\nACGT\n")
	w2 := newTestWriter(t, secondPath, ">P2\nTGCA\n")
	err := publishWriters(map[string]*Writer{"COI-5P": w1, "MATK": w2})
	if err == nil {
		t.Fatal("expected publish error for MATK")
	}
	got, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("previous output not preserved: got %q, want %q", got, sentinel)
	}
	discardWriters(map[string]*Writer{"COI-5P": w1, "MATK": w2})
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected sentinel and untouched directory only, got %v", entryNames(entries))
	}
	for _, entry := range entries {
		if entry.Name() == "MATK.fasta" && !entry.IsDir() {
			t.Fatal("expected untouched MATK.fasta directory to survive rollback")
		}
	}
}

func TestCommitFinalizesAllBeforePublishing(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full not available")
	}
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "COI-5P.fasta")
	goodTmp := goodPath + ".tmp"
	goodFile, err := os.Create(goodTmp)
	if err != nil {
		t.Fatal(err)
	}
	good := &Writer{file: goodFile, buf: bufio.NewWriterSize(goodFile, 64), path: goodPath, tmpPath: goodTmp}
	if _, err := good.buf.WriteString(">P1\nACGT\n"); err != nil {
		t.Fatal(err)
	}
	badFile, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	bad := &Writer{file: badFile, buf: bufio.NewWriterSize(badFile, 64), path: filepath.Join(dir, "MATK.fasta"), tmpPath: filepath.Join(dir, "MATK.fasta.tmp")}
	if _, err := bad.buf.WriteString(">P2\nTGCA\n"); err != nil {
		t.Fatal(err)
	}
	err = commitWriters(map[string]*Writer{"COI-5P": good, "MATK": bad})
	if err == nil || !strings.Contains(err.Error(), "flush marker MATK") {
		t.Fatalf("expected flush error for MATK, got %v", err)
	}
	if _, err := os.Stat(goodPath); !os.IsNotExist(err) {
		t.Fatalf("expected COI-5P to remain unpublished, got err=%v", err)
	}
	if _, err := os.Stat(goodTmp); err != nil {
		t.Fatalf("expected COI-5P temp to be intact, got err=%v", err)
	}
}

func TestPublishRollbackRestoresDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	firstPath := filepath.Join(dir, "COI-5P.fasta")
	secondPath := filepath.Join(dir, "MATK.fasta")
	linkTarget := "missing.fasta"
	if err := os.Symlink(linkTarget, firstPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Mkdir(secondPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writers := map[string]*Writer{
		"COI-5P": newTestWriter(t, firstPath, ">P1\nACGT\n"),
		"MATK":   newTestWriter(t, secondPath, ">P2\nTGCA\n"),
	}
	defer discardWriters(writers)
	if err := publishWriters(writers); err == nil {
		t.Fatal("expected publish error")
	}
	got, err := os.Readlink(firstPath)
	if err != nil {
		t.Fatalf("original symlink not restored: %v", err)
	}
	if got != linkTarget {
		t.Fatalf("symlink target = %q, want %q", got, linkTarget)
	}
	for name, w := range writers {
		if w.committed {
			t.Fatalf("marker %s remains marked committed after rollback", name)
		}
	}
	discardWriters(writers)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected original symlink and blocking directory, got %v", entryNames(entries))
	}
}

func TestPublishBackupFailureKeepsTarget(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based backup failure is not effective as root")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "MATK.fasta")
	sentinel := "sentinel MATK data"
	if err := os.WriteFile(target, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	w := newTestWriter(t, target, ">P1\nACGT\n")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err := publishWriters(map[string]*Writer{"MATK": w})
	if err == nil {
		t.Fatal("expected backup error")
	}
	if !strings.Contains(err.Error(), "backup marker MATK") {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != sentinel {
		t.Fatalf("sentinel destroyed by rollback: got %q, want %q", got, sentinel)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected sentinel and temp only, got %v", entryNames(entries))
	}
}

func TestPublishOverwritesWithStaleBakDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "MATK.fasta")
	if err := os.WriteFile(target, []byte("sentinel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target+".bak", 0o755); err != nil {
		t.Fatal(err)
	}
	w := newTestWriter(t, target, ">P1\nACGT\n")
	if err := publishWriters(map[string]*Writer{"MATK": w}); err != nil {
		t.Fatalf("publishWriters: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P1\nACGT\n" {
		t.Fatalf("got %q, want %q", got, ">P1\nACGT\n")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected target and stale .bak dir only, got %v", entryNames(entries))
	}
}

func TestBuildPreservesStaleBackupFile(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.tsv")
	outDir := filepath.Join(dir, "markers")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outDir, "MATK.fasta")
	if err := os.WriteFile(target, []byte("sentinel MATK output"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(outDir, fmt.Sprintf("MATK.fasta.bak-%d", os.Getpid()))
	staleContent := "STALE-BACKUP-SENTINEL"
	if err := os.WriteFile(stale, []byte(staleContent), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMarkerFixture(t, input, "ACGT", "TGCA")
	if err := BuildMarkerFastas(input, outDir, false, 0, -1, 2, ParseSelection("MATK")); err != nil {
		t.Fatalf("BuildMarkerFastas: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P2\nTGCA\n" {
		t.Fatalf("got %q, want %q", got, ">P2\nTGCA\n")
	}
	staleGot, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(staleGot) != staleContent {
		t.Fatalf("stale backup file destroyed: got %q, want %q", staleGot, staleContent)
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected target and stale backup only, got %v", entryNames(entries))
	}
}

func newTestWriter(t *testing.T, path, content string) *Writer {
	t.Helper()
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	return &Writer{file: f, buf: bufio.NewWriterSize(f, 64), path: path, tmpPath: tmpPath}
}

func TestCommitPublishesTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "COI-5P.fasta")
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	w := &Writer{file: f, buf: bufio.NewWriterSize(f, 64), path: path, tmpPath: tmpPath}
	if _, err := w.buf.WriteString(">P1\nACGT\n"); err != nil {
		t.Fatal(err)
	}
	if err := commitWriters(map[string]*Writer{"COI-5P": w}); err != nil {
		t.Fatalf("commitWriters: %v", err)
	}
	if !w.committed {
		t.Fatal("expected writer to be marked committed")
	}
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be renamed away, got err=%v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ">P1\nACGT\n" {
		t.Fatalf("got %q, want %q", got, ">P1\nACGT\n")
	}
}

func TestDiscardRemovesTempFiles(t *testing.T) {
	dir := t.TempDir()
	tmpPath := filepath.Join(dir, "MATK.fasta.tmp")
	f, err := os.Create(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	w := &Writer{file: f, buf: bufio.NewWriterSize(f, 64), path: filepath.Join(dir, "MATK.fasta"), tmpPath: tmpPath}
	discardWriters(map[string]*Writer{"MATK": w})
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp file to be removed, got err=%v", err)
	}
	if err := f.Close(); err == nil {
		t.Fatal("expected file to be closed by discard")
	}
}

func TestCommitPropagatesWriterErrors(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("/dev/full not available")
	}
	dir := t.TempDir()

	rawPath := filepath.Join(dir, "raw.fasta")
	rawFile, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw := &Writer{file: rawFile, buf: bufio.NewWriterSize(rawFile, 64), path: rawPath, tmpPath: rawPath + ".tmp"}
	if _, err := raw.buf.WriteString(">P1\nACGT\n"); err != nil {
		t.Fatal(err)
	}
	err = commitWriters(map[string]*Writer{"COI-5P": raw})
	if err == nil || !strings.Contains(err.Error(), "flush marker COI-5P") {
		t.Fatalf("expected flush error, got %v", err)
	}

	gzPath := filepath.Join(dir, "gz.fasta.gz")
	gzFile, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	pw, err := pgzip.NewWriterLevel(gzFile, pgzip.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if err := pw.SetConcurrency(1<<20, 2); err != nil {
		t.Fatal(err)
	}
	gz := &Writer{file: gzFile, buf: bufio.NewWriterSize(pw, 64), gz: pw, path: gzPath, tmpPath: gzPath + ".tmp"}
	if _, err := gz.buf.WriteString(">P1\nACGT\n"); err != nil {
		t.Fatal(err)
	}
	err = commitWriters(map[string]*Writer{"MATK": gz})
	if err == nil || !strings.Contains(err.Error(), "MATK") {
		t.Fatalf("expected gzip finalization error for MATK, got %v", err)
	}
}

func writeMarkerFixture(t *testing.T, path, coi, matk string) {
	t.Helper()
	content := "processid\tmarker_code\tnuc\nP1\tCOI-5P\t" + coi + "\nP2\tMATK\t" + matk + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertMarkerFasta(t *testing.T, dir, marker string, compressed bool, want string) {
	t.Helper()
	path := filepath.Join(dir, marker+".fasta")
	if compressed {
		path += ".gz"
	}
	in, err := boldio.OpenInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	got, err := io.ReadAll(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s: got %q, want %q", path, got, want)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
