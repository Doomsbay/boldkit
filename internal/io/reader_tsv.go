package io

import "fmt"

func parseTSVRows(path string, opts Options, onRow func(Row) error) error {
	in, err := OpenInput(path)
	if err != nil {
		return fmt.Errorf("open input %s: %w", path, err)
	}
	defer func() { _ = in.Close() }()
	return ParseTSV(in, opts, onRow)
}
