// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package java

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endRecord returns an end of central directory record naming the given directory size, records and offset.
func endRecord(records uint16, size, offset uint32) []byte {
	b := make([]byte, zipEndLen)
	copy(b, zipEndSignature)
	binary.LittleEndian.PutUint16(b[8:], records)
	binary.LittleEndian.PutUint16(b[10:], records)
	binary.LittleEndian.PutUint32(b[zipDirectorySizeAt:], size)
	binary.LittleEndian.PutUint32(b[16:], offset)
	return b
}

// directoryBomb returns an archive of n central directory records with no entry data.
func directoryBomb(n int) []byte {
	var b bytes.Buffer
	record := make([]byte, 46)
	copy(record, "PK\x01\x02")
	for range n {
		b.Write(record)
	}
	size := b.Len()
	b.Write(endRecord(0xffff, uint32(size), 0))
	return b.Bytes()
}

func TestScanArchive_ADirectoryOverTheBoundIsNotParsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jar")
	require.NoError(t, os.WriteFile(path, directoryBomb(400_000), 0o600))

	extractor := NewExtractor()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	require.NoError(t, extractor.scanArchive(context.Background(), path))
	runtime.ReadMemStats(&after)

	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
	assert.Zero(t, extractor.classesScanned)
}

func TestCentralDirectorySize_Zip64RecordIsRead(t *testing.T) {
	var b bytes.Buffer
	b.Write(make([]byte, 100))
	recordAt := b.Len()
	record := make([]byte, zip64EndLen)
	copy(record, zip64EndSignature)
	binary.LittleEndian.PutUint64(record[zip64DirectorySizeAt:], 1<<30)
	b.Write(record)
	locator := make([]byte, zip64LocatorLen)
	copy(locator, zip64LocatorSig)
	binary.LittleEndian.PutUint64(locator[8:], uint64(recordAt))
	b.Write(locator)
	b.Write(endRecord(0xffff, 0xffffffff, 0xffffffff))

	size, err := centralDirectorySize(bytes.NewReader(b.Bytes()), int64(b.Len()))
	require.NoError(t, err)
	assert.Equal(t, uint64(1<<30), size)
}

func TestCentralDirectorySize_AnArchiveWithoutAnEndRecordIsAnError(t *testing.T) {
	_, err := centralDirectorySize(bytes.NewReader([]byte("not an archive")), 14)
	require.Error(t, err)
}

func TestCentralDirectorySize_AWrittenArchiveIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.jar")
	writeZip(t, path, []zipEntry{{name: "com/example/A.class", data: []byte("x")}})
	data, err := os.ReadFile(path)
	require.NoError(t, err)

	size, err := centralDirectorySize(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	assert.Equal(t, uint64(46+len("com/example/A.class")), size)
}
