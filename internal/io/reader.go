package io

import (
	"path/filepath"
	"strings"
)

func IsParquetPath(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".parquet" || ext == ".parq"
}

func ParseRows(path string, opts Options, onRow func(Row) error) error {
	if IsParquetPath(path) {
		return parseParquet(path, opts, onRow)
	}
	return parseTSVRows(path, opts, onRow)
}

func RowCount(path string) (int64, error) {
	if IsParquetPath(path) {
		return parquetRowCount(path)
	}
	n, err := CountLines(path)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		return int64(n - 1), nil
	}
	return 0, nil
}

func InputFormat(path string) string {
	if IsParquetPath(path) {
		return "parquet"
	}
	return "tsv"
}
