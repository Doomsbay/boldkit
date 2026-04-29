package io

import (
	"bufio"
	"os"
)

const writerBufferSize = 1 << 20

type BufferedFile struct {
	W *bufio.Writer
	f *os.File
}

func CreateBuffered(path string) (*BufferedFile, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &BufferedFile{
		W: bufio.NewWriterSize(f, writerBufferSize),
		f: f,
	}, nil
}

func (bf *BufferedFile) Close() error {
	if err := bf.W.Flush(); err != nil {
		return err
	}
	return bf.f.Close()
}
