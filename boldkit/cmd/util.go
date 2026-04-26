package cmd

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func outputsExist(outDir string) bool {
	if _, err := os.Stat(outDir); err != nil {
		return false
	}
	if files, _ := filepath.Glob(filepath.Join(outDir, "*.fasta")); len(files) > 0 {
		return true
	}
	if files, _ := filepath.Glob(filepath.Join(outDir, "*.fasta.gz")); len(files) > 0 {
		return true
	}
	return false
}

func snapshotID(inputPath string) string {
	base := filepath.Base(inputPath)
	if strings.HasSuffix(base, ".parquet") {
		return strings.TrimSuffix(base, ".parquet")
	}
	if strings.HasSuffix(base, ".parq") {
		return strings.TrimSuffix(base, ".parq")
	}
	if strings.HasSuffix(base, ".tsv.gz") {
		return strings.TrimSuffix(base, ".tsv.gz")
	}
	if strings.HasSuffix(base, ".tsv") {
		return strings.TrimSuffix(base, ".tsv")
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func normalizeBytes(value []byte) []byte {
	if isNone(value) {
		return nil
	}
	return value
}

func fieldBytes(fields [][]byte, idx int) []byte {
	if idx < 0 || idx >= len(fields) {
		return nil
	}
	return fields[idx]
}

func indexOfBytes(values [][]byte, name string) int {
	for i, v := range values {
		if string(v) == name {
			return i
		}
	}
	return -1
}

func filterSeqBytes(dst []byte, src []byte) []byte {
	dst = dst[:0]
	for _, c := range src {
		switch c {
		case 'A', 'C', 'G', 'T':
			dst = append(dst, c)
		case 'a', 'c', 'g', 't':
			dst = append(dst, c-32)
		}
	}
	return dst
}

func sanitizeMarkerBytes(dst []byte, src []byte) string {
	dst = dst[:0]
	dst = append(dst, src...)
	for i := 0; i < len(dst); i++ {
		c := dst[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-' {
			continue
		}
		dst[i] = '_'
	}
	return string(dst)
}

func maxIndex(values ...int) int {
	max := -1
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	return max
}

func countLines(path string) (int, error) {
	in, err := openInput(path)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = in.Close()
	}()

	buf := make([]byte, 1024*1024)
	var count int
	var lastByte byte
	for {
		n, err := in.Read(buf)
		if n > 0 {
			count += bytes.Count(buf[:n], []byte{'\n'})
			lastByte = buf[n-1]
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	if lastByte != '\n' && lastByte != 0 {
		count++
	}
	return count, nil
}

type readCloser struct {
	reader io.Reader
	close  func() error
}

func (r readCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r readCloser) Close() error {
	return r.close()
}

type countReader struct {
	reader io.Reader
	count  int64
}

func (r *countReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (r *countReader) Count() int64 {
	return r.count
}

func openInput(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		return readCloser{
			reader: gz,
			close: func() error {
				_ = gz.Close()
				return f.Close()
			},
		}, nil
	}
	return f, nil
}

func openInputWithCounter(path string) (io.ReadCloser, *countReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	counter := &countReader{reader: f}
	if strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(counter)
		if err != nil {
			_ = f.Close()
			return nil, nil, err
		}
		return readCloser{
			reader: gz,
			close: func() error {
				_ = gz.Close()
				return f.Close()
			},
		}, counter, nil
	}
	return readCloser{
		reader: counter,
		close:  f.Close,
	}, counter, nil
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

func isNone(b []byte) bool {
	return len(b) == 4 && b[0] == 'N' && b[1] == 'o' && b[2] == 'n' && b[3] == 'e'
}
