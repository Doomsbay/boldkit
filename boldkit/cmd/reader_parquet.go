package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
	"github.com/apache/arrow/go/v18/parquet"
	"github.com/apache/arrow/go/v18/parquet/file"
	"github.com/apache/arrow/go/v18/parquet/pqarrow"
)

func parseParquet(path string, opts Options, onRow func(Row) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open parquet %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	mem := memory.NewGoAllocator()
	pf, err := file.NewParquetReader(parquet.ReaderAtSeeker(f), file.WithReadProps(parquet.NewReaderProperties(mem)))
	if err != nil {
		return fmt.Errorf("open parquet file: %w", err)
	}
	defer func() { _ = pf.Close() }()

	schema := pf.MetaData().Schema
	numCols := schema.NumColumns()

	header := make([][]byte, numCols)
	for i := 0; i < numCols; i++ {
		header[i] = []byte(schema.Column(i).Name())
	}
	if err := onRow(Row{Line: 0, Fields: header}); err != nil {
		return err
	}

	fr, err := pqarrow.NewFileReader(pf, pqarrow.ArrowReadProperties{}, mem)
	if err != nil {
		return fmt.Errorf("create arrow file reader: %w", err)
	}

	ctx := context.Background()
	colIndices := make([]int, numCols)
	for i := range colIndices {
		colIndices[i] = i
	}

	lineNum := int64(0)
	for rgIdx := 0; rgIdx < pf.NumRowGroups(); rgIdx++ {
		tbl, err := fr.ReadRowGroups(ctx, colIndices, []int{rgIdx})
		if err != nil {
			return fmt.Errorf("read row group %d: %w", rgIdx, err)
		}

		nRows := int(tbl.NumRows())
		nCols := int(tbl.NumCols())
		cols := make([]arrow.Array, nCols)
		for c := 0; c < nCols; c++ {
			chunks := tbl.Column(c).Data()
			if chunks.Len() > 0 {
				cols[c] = chunks.Chunk(0)
			}
		}

		for r := 0; r < nRows; r++ {
			lineNum++
			fields := make([][]byte, numCols)
			for c := 0; c < nCols; c++ {
				if cols[c] != nil {
					fields[c] = columnStringValue(cols[c], r)
				}
			}
			if opts.Progress != nil {
				if !opts.SkipProgressFirstRow || lineNum != 1 {
					opts.Progress.increment()
				}
			}
			if err := onRow(Row{Line: lineNum, Fields: fields}); err != nil {
				tbl.Release()
				return err
			}
		}
		tbl.Release()
	}

	return nil
}

func columnStringValue(col arrow.Array, row int) []byte {
	if col.IsNull(row) {
		return nil
	}
	switch c := col.(type) {
	case *array.String:
		return []byte(c.Value(row))
	case *array.Binary:
		return c.Value(row)
	case *array.Int64:
		return []byte(fmt.Sprintf("%d", c.Value(row)))
	case *array.Float64:
		return []byte(fmt.Sprintf("%g", c.Value(row)))
	default:
		return []byte(col.ValueStr(row))
	}
}

func parquetRowCount(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()

	pf, err := file.NewParquetReader(parquet.ReaderAtSeeker(f))
	if err != nil {
		return 0, fmt.Errorf("open parquet file: %w", err)
	}
	defer func() { _ = pf.Close() }()

	return pf.NumRows(), nil
}
