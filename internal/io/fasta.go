package io

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

type FASTARecord struct {
	ID  string
	Seq []byte
}

func ParseFasta(r io.Reader, onRecord func(FASTARecord) error) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var header string
	var seq bytes.Buffer
	emit := func() error {
		if header == "" {
			return nil
		}
		rec := FASTARecord{
			ID:  fastaID(header),
			Seq: append([]byte(nil), seq.Bytes()...),
		}
		seq.Reset()
		header = ""
		return onRecord(rec)
	}

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, ">") {
			if err := emit(); err != nil {
				return err
			}
			header = strings.TrimSpace(line[1:])
			continue
		}
		seq.WriteString(strings.TrimSpace(line))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan fasta: %w", err)
	}
	if err := emit(); err != nil {
		return err
	}
	return nil
}

func fastaID(header string) string {
	if header == "" {
		return ""
	}
	fields := strings.Fields(header)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func WriteFasta(w *bufio.Writer, header string, seq []byte) error {
	if _, err := w.WriteString(">" + header + "\n"); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := w.Write(seq); err != nil {
		return fmt.Errorf("write seq: %w", err)
	}
	if _, err := w.WriteString("\n"); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}
	return nil
}
