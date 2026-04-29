package io

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func FileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}

func CountLines(path string) (int, error) {
	in, err := OpenInput(path)
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

func CopyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create %s: %w", dest, err)
	}
	defer func() {
		_ = out.Close()
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return nil
}

func CopyDir(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", src)
	}
	if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
		return fmt.Errorf("create dir %s: %w", dest, err)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks not supported in move fallback")
		}
		return CopyFile(path, target)
	})
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
