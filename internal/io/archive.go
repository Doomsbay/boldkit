package io

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func PackageDirGzip(srcDir, destTarGz string, force bool) error {
	if FileExists(destTarGz) && !force {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(destTarGz), 0o755); err != nil {
		return fmt.Errorf("create releases dir: %w", err)
	}

	out, err := os.Create(destTarGz)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer func() {
		_ = out.Close()
	}()

	gzw, err := gzip.NewWriterLevel(out, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("create gzip writer: %w", err)
	}
	tw := tar.NewWriter(gzw)

	base := filepath.Base(srcDir)
	if err := filepath.Walk(srcDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(filepath.Join(base, rel))
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, in)
		_ = in.Close()
		return err
	}); err != nil {
		_ = tw.Close()
		_ = gzw.Close()
		return err
	}

	if err := tw.Close(); err != nil {
		_ = gzw.Close()
		return err
	}
	if err := gzw.Close(); err != nil {
		return err
	}
	return nil
}
