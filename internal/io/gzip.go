package io

import (
	"compress/gzip"
	"io"
	"os"
	"strings"
)

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

type CountReader struct {
	reader io.Reader
	count  int64
}

func (r *CountReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (r *CountReader) Count() int64 {
	return r.count
}

func OpenInput(path string) (io.ReadCloser, error) {
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

func OpenInputWithCounter(path string) (io.ReadCloser, *CountReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	counter := &CountReader{reader: f}
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
