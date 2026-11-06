package io

import (
	"fmt"
	"os"
)

type AtomicFile struct {
	f    *os.File
	dest string
	tmp  string
}

func CreateAtomic(dest string) (*AtomicFile, error) {
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", tmp, err)
	}
	return &AtomicFile{f: f, dest: dest, tmp: tmp}, nil
}

func (a *AtomicFile) Write(p []byte) (int, error) {
	return a.f.Write(p)
}

func (a *AtomicFile) Commit() error {
	if err := a.f.Sync(); err != nil {
		a.cleanup()
		return fmt.Errorf("sync %s: %w", a.tmp, err)
	}
	if err := a.f.Close(); err != nil {
		a.f = nil
		a.cleanup()
		return fmt.Errorf("close %s: %w", a.tmp, err)
	}
	a.f = nil
	if err := os.Rename(a.tmp, a.dest); err != nil {
		a.cleanup()
		return fmt.Errorf("publish %s: %w", a.dest, err)
	}
	return nil
}

func (a *AtomicFile) Abort() {
	if a.f != nil {
		_ = a.f.Close()
		a.f = nil
	}
	a.cleanup()
}

func (a *AtomicFile) cleanup() {
	_ = os.Remove(a.tmp)
}
