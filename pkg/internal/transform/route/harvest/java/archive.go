// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package java // import "go.opentelemetry.io/obi/pkg/internal/transform/route/harvest/java"

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (e *Extractor) scanDir(ctx context.Context, root string) error {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err != nil {
			e.log.Debug("error walking Java classpath directory", "path", path, "error", err)
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if e.classLimitReached() || e.routeLimitReached() {
			return filepath.SkipAll
		}
		if strings.EqualFold(filepath.Ext(path), ".class") {
			if err := e.scanClassFile(ctx, path); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		e.log.Debug("error scanning Java classpath directory", "path", root, "error", err)
	}
	return nil
}

func (e *Extractor) scanClassFile(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		e.log.Debug("error stating Java class file", "path", path, "error", err)
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.Size() > MaxJavaClassScanBytes {
		e.log.Info("java class file scan limit reached", "path", path, "size", info.Size(), "limit", MaxJavaClassScanBytes)
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		e.log.Debug("error reading Java class file", "path", path, "error", err)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return e.scanClassBytes(ctx, path, data)
}

func (e *Extractor) scanArchive(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		e.log.Debug("error stating Java archive", "path", path, "error", err)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.Size() > MaxJavaArchiveScanBytes {
		e.log.Info("java archive scan limit reached", "path", path, "size", info.Size(), "limit", MaxJavaArchiveScanBytes)
		return nil
	}

	archive, err := os.Open(path)
	if err != nil {
		e.log.Debug("error opening Java archive", "path", path, "error", err)
		return nil
	}
	defer func() {
		if err := archive.Close(); err != nil {
			e.log.Debug("error closing Java archive", "path", path, "error", err)
		}
	}()
	directory, err := centralDirectorySize(archive, info.Size())
	if err != nil {
		e.log.Debug("error reading Java archive directory", "path", path, "error", err)
		return nil
	}
	if directory > MaxJavaArchiveDirectoryBytes {
		e.log.Info("java archive directory limit reached", "path", path, "size", directory, "limit", MaxJavaArchiveDirectoryBytes)
		return nil
	}
	reader, err := zip.NewReader(archive, info.Size())
	if err != nil {
		e.log.Debug("error opening Java archive", "path", path, "error", err)
		return nil
	}

	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.classLimitReached() || e.routeLimitReached() {
			return nil
		}
		if !scanArchiveClassEntry(file.Name) {
			continue
		}
		if file.UncompressedSize64 > uint64(MaxJavaClassScanBytes) {
			e.log.Info("java class file scan limit reached", "path", file.Name, "size", file.UncompressedSize64, "limit", MaxJavaClassScanBytes)
			continue
		}
		if err := e.scanZipClass(ctx, path, file); err != nil {
			return err
		}
	}
	return nil
}

func scanArchiveClassEntry(name string) bool {
	name = filepath.ToSlash(name)
	if !strings.HasSuffix(name, ".class") {
		return false
	}
	if strings.HasPrefix(name, "BOOT-INF/lib/") || strings.HasPrefix(name, "WEB-INF/lib/") {
		return false
	}
	if strings.HasPrefix(name, "META-INF/") {
		return false
	}
	return true
}

func (e *Extractor) scanZipClass(ctx context.Context, archivePath string, file *zip.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	reader, err := file.Open()
	if err != nil {
		e.log.Debug("error opening Java class entry", "archive", archivePath, "path", file.Name, "error", err)
		return nil
	}
	defer func() {
		if err := reader.Close(); err != nil {
			e.log.Debug("error closing Java class entry", "archive", archivePath, "path", file.Name, "error", err)
		}
	}()

	data, err := io.ReadAll(io.LimitReader(reader, MaxJavaClassScanBytes+1))
	if err != nil {
		e.log.Debug("error reading Java class entry", "archive", archivePath, "path", file.Name, "error", err)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if int64(len(data)) > MaxJavaClassScanBytes {
		e.log.Info("java class file scan limit reached", "path", file.Name, "size", len(data), "limit", MaxJavaClassScanBytes)
		return nil
	}
	return e.scanClassBytes(ctx, archivePath+":"+file.Name, data)
}

func (e *Extractor) scanClassBytes(ctx context.Context, name string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.classesScanned++

	class, err := parseClassFile(data)
	if err != nil {
		e.log.Debug("error parsing Java class file", "path", name, "error", err)
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	e.addRoutes(routesFromClass(class))
	return nil
}

const (
	zipEndLen            = 22
	zipEndCommentMax     = 65535
	zip64LocatorLen      = 20
	zip64EndLen          = 56
	zipEndSignature      = "PK\x05\x06"
	zip64LocatorSig      = "PK\x06\x07"
	zip64EndSignature    = "PK\x06\x06"
	zipDirectorySizeAt   = 12
	zip64DirectorySizeAt = 40
)

// centralDirectorySize reads the size of an archive's central directory from its end records, the zip64 record when the end record defers to it.
func centralDirectorySize(r io.ReaderAt, size int64) (uint64, error) {
	tailLen := min(size, zipEndLen+zipEndCommentMax)
	tail := make([]byte, tailLen)
	if _, err := r.ReadAt(tail, size-tailLen); err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	at := bytes.LastIndex(tail, []byte(zipEndSignature))
	if at < 0 || len(tail)-at < zipEndLen {
		return 0, errors.New("no end of central directory record")
	}
	directory := uint64(binary.LittleEndian.Uint32(tail[at+zipDirectorySizeAt:]))
	if directory != 0xffffffff {
		return directory, nil
	}
	endAt := size - tailLen + int64(at)
	if endAt < zip64LocatorLen {
		return 0, errors.New("no zip64 end of central directory locator")
	}
	var locator [zip64LocatorLen]byte
	if _, err := r.ReadAt(locator[:], endAt-zip64LocatorLen); err != nil {
		return 0, err
	}
	if string(locator[:4]) != zip64LocatorSig {
		return 0, errors.New("no zip64 end of central directory locator")
	}
	recordAt := binary.LittleEndian.Uint64(locator[8:])
	if recordAt > uint64(size) || uint64(size)-recordAt < zip64EndLen {
		return 0, errors.New("zip64 end of central directory record outside the archive")
	}
	var record [zip64EndLen]byte
	if _, err := r.ReadAt(record[:], int64(recordAt)); err != nil {
		return 0, err
	}
	if string(record[:4]) != zip64EndSignature {
		return 0, errors.New("no zip64 end of central directory record")
	}
	return binary.LittleEndian.Uint64(record[zip64DirectorySizeAt:]), nil
}
