package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/apache/arrow/go/v18/parquet"
	"github.com/apache/arrow/go/v18/parquet/compress"
	"github.com/apache/arrow/go/v18/parquet/pqarrow"
)

func TestParquetRowCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.parquet")

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "processid", Type: arrow.BinaryTypes.String},
		{Name: "marker_code", Type: arrow.BinaryTypes.String},
		{Name: "nuc", Type: arrow.BinaryTypes.String},
	}, nil)

	table := array.NewRecordBuilder(memory.NewGoAllocator(), schema)
	defer table.Release()

	for i := 0; i < 100; i++ {
		table.Field(0).(*array.StringBuilder).Append("TEST001")
		table.Field(1).(*array.StringBuilder).Append("COI-5P")
		table.Field(2).(*array.StringBuilder).Append("ACGTACGT")
	}

	rec := table.NewRecord()
	defer rec.Release()

	props := parquet.NewWriterProperties(parquet.WithCompression(compress.Codecs.Zstd))
	pw, err := pqarrow.NewFileWriter(schema, f, props, pqarrow.DefaultWriterProps())
	if err != nil {
		t.Fatal(err)
	}

	if err := pw.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := pw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	count, err := parquetRowCount(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 100 {
		t.Errorf("expected 100 rows, got %d", count)
	}
}

func TestParquetReadsRealFile(t *testing.T) {
	realFile := "BOLD_Public.27-Mar-2026.parquet"
	if _, err := os.Stat(realFile); err != nil {
		t.Skip("skipping: real BOLD parquet file not present")
	}

	count, err := parquetRowCount(realFile)
	if err != nil {
		t.Fatalf("parquetRowCount: %v", err)
	}
	if count <= 0 {
		t.Fatalf("expected positive row count, got %d", count)
	}

	opts := DefaultOptions()
	headerSeen := false
	dataRows := int64(0)

	err = ParseRows(realFile, opts, func(row Row) error {
		if !headerSeen {
			headerSeen = true
			hasProcessID := false
			hasMarkerCode := false
			for _, f := range row.Fields {
				if string(f) == "processid" {
					hasProcessID = true
				}
				if string(f) == "marker_code" {
					hasMarkerCode = true
				}
			}
			if !hasProcessID || !hasMarkerCode {
				return fmt.Errorf("missing expected columns in parquet header")
			}
			return nil
		}
		dataRows++
		if dataRows >= 10 {
			return fmt.Errorf("stop")
		}
		return nil
	})
	if err != nil && err.Error() != "stop" {
		t.Fatalf("ParseRows: %v", err)
	}
	if dataRows < 10 {
		t.Errorf("expected at least 10 data rows, got %d", dataRows)
	}
}

func TestIsParquetPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"data.parquet", true},
		{"data.PARQUET", true},
		{"data.parq", true},
		{"data.PARQ", true},
		{"data.tsv", false},
		{"data.tsv.gz", false},
		{"data.csv", false},
	}
	for _, tt := range tests {
		got := isParquetPath(tt.path)
		if got != tt.want {
			t.Errorf("isParquetPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
