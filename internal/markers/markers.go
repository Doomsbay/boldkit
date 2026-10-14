package markers

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/klauspost/pgzip"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	boldprogress "github.com/Doomsbay/BoldKit/internal/progress"
	"github.com/Doomsbay/BoldKit/internal/util"
)

const writerBufferSize = 1 << 20

type Writer struct {
	file      *os.File
	buf       *bufio.Writer
	gz        io.Closer
	path      string
	tmpPath   string
	committed bool
}

func ParseSelection(raw string) [][]byte {
	names := util.SplitList(raw)
	selected := make([][]byte, 0, len(names))
	for _, name := range names {
		selected = append(selected, []byte(name))
	}
	return selected
}

func SelectMissing(outDir string, selected [][]byte, force bool) ([][]byte, bool, error) {
	if force {
		return selected, false, nil
	}
	if len(selected) == 0 {
		return selected, outputsExist(outDir), nil
	}
	entries, err := os.ReadDir(outDir)
	if os.IsNotExist(err) {
		return selected, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	missing := make([][]byte, 0, len(selected))
	for _, marker := range selected {
		name := util.SanitizeMarkerBytes(nil, marker)
		found := false
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			filename := strings.TrimSuffix(entry.Name(), ".gz")
			if strings.EqualFold(filename, name+".fasta") {
				found = true
				break
			}
		}
		if found {
			util.Logf("markers: FASTA for %q exists, skipping (use --force to overwrite)", marker)
		} else {
			missing = append(missing, marker)
		}
	}
	return missing, len(missing) == 0, nil
}

func BuildMarkerFastas(inputPath, outDir string, gzipOut bool, reportEvery, totalRows, workers int, selected [][]byte) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	writers := make(map[string]*Writer)
	counts := make(map[string]int)
	committed := false
	defer func() {
		if !committed {
			discardWriters(writers)
		}
	}()

	progress := boldprogress.New(totalRows, reportEvery)
	var (
		idxProcess = -1
		idxMarker  = -1
		idxNuc     = -1
	)

	opts := boldio.DefaultOptions()
	opts.StrictColumns = true
	opts.BatchLines = 2048
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	gzipWorkers := workers
	opts.Workers = workers
	opts.Progress = progress
	opts.SkipProgressFirstRow = true

	seqPool := sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 2048)
			return &buf
		},
	}
	recordPool := sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 4096)
			return &buf
		},
	}
	markerBufPool := sync.Pool{
		New: func() any {
			buf := make([]byte, 0, 32)
			return &buf
		},
	}

	err := boldio.ParseRows(inputPath, opts, func(row boldio.Row) error {
		if idxProcess < 0 {
			idxProcess = util.IndexOfBytes(row.Fields, "processid")
			idxMarker = util.IndexOfBytes(row.Fields, "marker_code")
			idxNuc = util.IndexOfBytes(row.Fields, "nuc")
			if idxProcess < 0 || idxMarker < 0 || idxNuc < 0 {
				return errors.New("required headers missing in input TSV")
			}
			return nil
		}

		fields := row.Fields
		if idxProcess >= len(fields) || idxMarker >= len(fields) || idxNuc >= len(fields) {
			return fmt.Errorf("line %d: expected at least %d fields", row.Line, util.MaxIndex(idxProcess, idxMarker, idxNuc)+1)
		}

		markerVal := util.Normalize(fields[idxMarker])
		if len(markerVal) == 0 {
			markerVal = []byte("UNKNOWN")
		}

		if len(selected) > 0 && !markerSelected(markerVal, selected) {
			return nil
		}

		nuc := fields[idxNuc]
		if util.IsNull(nuc) {
			nuc = nil
		}

		seqBufPtr := seqPool.Get().(*[]byte)
		seqBuf := *seqBufPtr
		seq := util.PrepareSeqBytes(seqBuf, nuc)

		markerScratchPtr := markerBufPool.Get().(*[]byte)
		markerScratch := *markerScratchPtr
		sanitizedMarker := util.SanitizeMarkerBytes(markerScratch, markerVal)
		*markerScratchPtr = markerScratch[:0]
		markerBufPool.Put(markerScratchPtr)

		pid := fields[idxProcess]
		w, err := getWriter(outDir, sanitizedMarker, gzipOut, gzipWorkers, writers)
		if err != nil {
			*seqBufPtr = seq[:0]
			seqPool.Put(seqBufPtr)
			return err
		}

		recordPtr := recordPool.Get().(*[]byte)
		record := *recordPtr
		record = append(record[:0], '>')
		record = append(record, pid...)
		record = append(record, '\n')
		record = append(record, seq...)
		record = append(record, '\n')

		if _, err := w.buf.Write(record); err != nil {
			*recordPtr = record[:0]
			recordPool.Put(recordPtr)
			*seqBufPtr = seq[:0]
			seqPool.Put(seqBufPtr)
			return fmt.Errorf("write marker %s: %w", sanitizedMarker, err)
		}
		counts[string(markerVal)]++

		*recordPtr = record[:0]
		recordPool.Put(recordPtr)
		*seqBufPtr = seq[:0]
		seqPool.Put(seqBufPtr)
		return nil
	})
	if err != nil {
		return err
	}
	progress.Finish()

	if err := commitWriters(writers); err != nil {
		return err
	}
	committed = true

	for _, sel := range selected {
		if !markerWritten(counts, string(sel)) {
			util.Logf("markers: warning: selected marker %q matched 0 records", string(sel))
		}
	}

	var totalRecords int
	for _, n := range counts {
		totalRecords += n
	}
	util.Logf("markers: wrote %d records across %d marker(s)", totalRecords, len(writers))

	return nil
}

func markerSelected(marker []byte, selected [][]byte) bool {
	for _, s := range selected {
		if bytes.EqualFold(marker, s) {
			return true
		}
	}
	return false
}

func markerWritten(counts map[string]int, name string) bool {
	for key := range counts {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func commitWriters(writers map[string]*Writer) error {
	if err := checkWriterTargets(writers); err != nil {
		return err
	}
	if err := finalizeWriters(writers); err != nil {
		return err
	}
	return publishWriters(writers)
}

func sortedWriterNames(writers map[string]*Writer) []string {
	names := make([]string, 0, len(writers))
	for name := range writers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func checkWriterTargets(writers map[string]*Writer) error {
	for _, name := range sortedWriterNames(writers) {
		w := writers[name]
		if st, err := os.Stat(w.path); err == nil && st.IsDir() {
			return fmt.Errorf("publish marker %s: %s is a directory", name, w.path)
		}
	}
	return nil
}

func finalizeWriters(writers map[string]*Writer) error {
	for _, name := range sortedWriterNames(writers) {
		w := writers[name]
		if err := w.buf.Flush(); err != nil {
			return fmt.Errorf("flush marker %s: %w", name, err)
		}
		if w.gz != nil {
			if err := w.gz.Close(); err != nil {
				w.gz = nil
				return fmt.Errorf("close gzip marker %s: %w", name, err)
			}
			w.gz = nil
		}
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("close marker %s: %w", name, err)
		}
	}
	return nil
}

type publishedWriter struct {
	writer    *Writer
	backup    string
	backedUp  bool
	published bool
}

func backupExisting(path string) (string, error) {
	reserved, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".bak-*")
	if err != nil {
		return "", err
	}
	name := reserved.Name()
	if err := reserved.Close(); err != nil {
		_ = os.Remove(name)
		return "", fmt.Errorf("reserve backup %s: %w", name, err)
	}
	if err := os.Rename(path, name); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func publishWriters(writers map[string]*Writer) error {
	published := make([]*publishedWriter, 0, len(writers))
	for _, name := range sortedWriterNames(writers) {
		w := writers[name]
		entry := &publishedWriter{writer: w}
		if st, err := os.Lstat(w.path); err == nil {
			if st.IsDir() {
				return rollbackPublished(published, entry, fmt.Errorf("publish marker %s: %s is a directory", name, w.path))
			}
			backup, err := backupExisting(w.path)
			if err != nil {
				return rollbackPublished(published, entry, fmt.Errorf("backup marker %s: %w", name, err))
			}
			entry.backup = backup
			entry.backedUp = true
		} else if !os.IsNotExist(err) {
			return rollbackPublished(published, entry, fmt.Errorf("inspect marker %s: %w", name, err))
		}
		if err := os.Rename(w.tmpPath, w.path); err != nil {
			return rollbackPublished(published, entry, fmt.Errorf("publish marker %s: %w", name, err))
		}
		entry.published = true
		published = append(published, entry)
		w.committed = true
	}
	for _, entry := range published {
		if entry.backedUp {
			_ = os.Remove(entry.backup)
		}
	}
	return nil
}

func rollbackPublished(published []*publishedWriter, failed *publishedWriter, cause error) error {
	entries := make([]*publishedWriter, 0, len(published)+1)
	entries = append(entries, published...)
	if failed != nil {
		entries = append(entries, failed)
	}
	var restoreErr error
	for _, entry := range entries {
		entry.writer.committed = false
		if entry.published {
			_ = os.Remove(entry.writer.path)
		}
		if !entry.backedUp {
			continue
		}
		if err := os.Rename(entry.backup, entry.writer.path); err != nil && restoreErr == nil {
			restoreErr = fmt.Errorf("restore marker %s: %w", entry.writer.path, err)
		}
	}
	if restoreErr != nil {
		return errors.Join(cause, restoreErr)
	}
	return cause
}

func discardWriters(writers map[string]*Writer) {
	for _, w := range writers {
		if w.committed {
			continue
		}
		if w.gz != nil {
			_ = w.gz.Close()
		}
		_ = w.file.Close()
		_ = os.Remove(w.tmpPath)
	}
}

func outputsExist(outDir string) bool {
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".gz")
		if strings.HasSuffix(name, ".fasta") {
			return true
		}
	}
	return false
}

func getWriter(outDir, marker string, gzipOut bool, gzipWorkers int, writers map[string]*Writer) (*Writer, error) {
	if w, ok := writers[marker]; ok {
		return w, nil
	}
	ext := ".fasta"
	if gzipOut {
		ext += ".gz"
	}
	path := filepath.Join(outDir, marker+ext)
	tmpPath := path + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", tmpPath, err)
	}
	var gz io.Closer
	var buf *bufio.Writer
	if gzipOut {
		if gzipWorkers <= 0 {
			gzipWorkers = runtime.GOMAXPROCS(0)
		}
		pw, err := pgzip.NewWriterLevel(f, pgzip.DefaultCompression)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("create gzip writer: %w", err)
		}
		if err := pw.SetConcurrency(1<<20, gzipWorkers); err != nil {
			_ = pw.Close()
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("set gzip concurrency: %w", err)
		}
		gz = pw
		buf = bufio.NewWriterSize(pw, writerBufferSize)
	} else {
		buf = bufio.NewWriterSize(f, writerBufferSize)
	}
	w := &Writer{file: f, buf: buf, gz: gz, path: path, tmpPath: tmpPath}
	writers[marker] = w
	return w, nil
}
