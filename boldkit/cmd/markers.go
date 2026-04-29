package cmd

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/klauspost/pgzip"

	boldio "github.com/Doomsbay/BoldKit/internal/io"
	"github.com/Doomsbay/BoldKit/internal/util"
)

type markerWriter struct {
	file *os.File
	buf  *bufio.Writer
	gz   io.Closer
}

func runMarkers(args []string) {
	fs := flag.NewFlagSet("markers", flag.ExitOnError)
	input := fs.String("input", "BOLD_Public.*/BOLD_Public.*.tsv", "BOLD input file (TSV or Parquet)")
	outDir := fs.String("outdir", "marker_fastas", "Output directory for marker FASTAs")
	progressOn := fs.Bool("progress", true, "Show progress bar")
	gzipOut := fs.Bool("gzip", true, "Compress FASTA outputs to .fasta.gz")
	force := fs.Bool("force", false, "Overwrite existing outputs")
	workers := fs.Int("workers", runtime.GOMAXPROCS(0), "Parser worker goroutines (<=0 defaults to GOMAXPROCS)")
	if err := fs.Parse(args); err != nil {
		fatalf("parse args failed: %v", err)
	}

	if !*force && outputsExist(*outDir) {
		fmt.Fprintf(os.Stderr, "Marker FASTAs already exist, skipping: %s\n", *outDir)
		return
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatalf("failed to create output dir: %v", err)
	}

	totalRows := -1
	if *progressOn {
		count, err := boldio.RowCount(*input)
		if err != nil {
			fatalf("count rows failed: %v", err)
		}
		totalRows = int(count)
	}

	reportEvery := 0
	if *progressOn {
		reportEvery = 1
	}

	if err := buildMarkerFastas(*input, *outDir, *gzipOut, reportEvery, totalRows, *workers); err != nil {
		fatalf("build failed: %v", err)
	}
}

func buildMarkerFastas(inputPath, outDir string, gzipOut bool, reportEvery, totalRows, workers int) error {
	writers := make(map[string]*markerWriter)
	defer func() {
		for _, w := range writers {
			_ = w.buf.Flush()
			if w.gz != nil {
				_ = w.gz.Close()
			}
			_ = w.file.Close()
		}
	}()

	progress := newProgress(totalRows, reportEvery)
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

		nuc := fields[idxNuc]
		if len(nuc) == 0 || util.IsNull(nuc) {
			return nil
		}

		seqBufPtr := seqPool.Get().(*[]byte)
		seqBuf := *seqBufPtr
		seq := util.FilterSeqBytes(seqBuf, nuc)
		if len(seq) == 0 {
			*seqBufPtr = seq[:0]
			seqPool.Put(seqBufPtr)
			return nil
		}

		markerVal := util.Normalize(fields[idxMarker])
		if len(markerVal) == 0 {
			markerVal = []byte("UNKNOWN")
		}

		markerScratchPtr := markerBufPool.Get().(*[]byte)
		markerScratch := *markerScratchPtr
		sanitizedMarker := util.SanitizeMarkerBytes(markerScratch, markerVal)
		*markerScratchPtr = markerScratch[:0]
		markerBufPool.Put(markerScratchPtr)

		pid := fields[idxProcess]
		w, err := getMarkerWriter(outDir, sanitizedMarker, gzipOut, gzipWorkers, writers)
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
	return nil
}

func getMarkerWriter(outDir, marker string, gzipOut bool, gzipWorkers int, writers map[string]*markerWriter) (*markerWriter, error) {
	if w, ok := writers[marker]; ok {
		return w, nil
	}
	ext := ".fasta"
	if gzipOut {
		ext += ".gz"
	}
	path := filepath.Join(outDir, marker+ext)
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
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
			return nil, fmt.Errorf("create gzip writer: %w", err)
		}
		if err := pw.SetConcurrency(1<<20, gzipWorkers); err != nil {
			_ = pw.Close()
			_ = f.Close()
			return nil, fmt.Errorf("set gzip concurrency: %w", err)
		}
		gz = pw
		buf = bufio.NewWriterSize(pw, writerBufferSize)
	} else {
		buf = bufio.NewWriterSize(f, writerBufferSize)
	}
	w := &markerWriter{file: f, buf: buf, gz: gz}
	writers[marker] = w
	return w, nil
}
