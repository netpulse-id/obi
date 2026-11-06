// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package elfcap

import (
	"bytes"
	"debug/elf"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/internal/elfcap/elfcaptest"
)

const declared = 256 << 20

func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func writeFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "exe")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestNewFileAndOpen_ACompressedNameTableIsRefusedBeforeItIsInflated(t *testing.T) {
	data := elfcaptest.BuildWithNameTable(elfcaptest.ZstdSection(declared))
	require.Less(t, len(data), 9<<10)

	var err error
	require.Less(t, allocatedBy(func() { _, err = NewFile(bytes.NewReader(data), int64(len(data))) }), uint64(1<<20))
	require.ErrorContains(t, err, "compressed section name table")

	path := writeFile(t, data)
	require.Less(t, allocatedBy(func() { _, err = Open(path) }), uint64(1<<20))
	require.ErrorContains(t, err, "compressed section name table")
}

func TestNewFileAndOpen_ACompressedSymbolTableSectionIsRefused(t *testing.T) {
	data := elfcaptest.Build(elfcaptest.Section{Name: ".gopclntab", Type: elf.SHT_PROGBITS, Flags: elf.SHF_COMPRESSED, Data: elfcaptest.ZstdSection(declared)})

	var err error
	require.Less(t, allocatedBy(func() { _, err = NewFile(bytes.NewReader(data), int64(len(data))) }), uint64(1<<20))
	require.ErrorContains(t, err, `".gopclntab" is not debug information`)

	_, err = Open(writeFile(t, data))
	require.ErrorContains(t, err, `".gopclntab" is not debug information`)
}

func TestNewFile_AWritableZdebugSectionIsRefused(t *testing.T) {
	data := elfcaptest.Build(elfcaptest.Section{Name: ".zdebug_data", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_WRITE, Data: elfcaptest.ZlibHeader(1 << 30)})
	_, err := NewFile(bytes.NewReader(data), int64(len(data)))
	require.ErrorContains(t, err, "not debug information")
}

func TestNewFileAndOpen_CompressedDebugInformationIsParsed(t *testing.T) {
	data := elfcaptest.Build(elfcaptest.Section{Name: ".debug_info", Type: elf.SHT_PROGBITS, Flags: elf.SHF_COMPRESSED, Data: elfcaptest.ZstdSection(1 << 20)})

	f, err := NewFile(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	require.NotNil(t, f.Section(".debug_info"))

	f, err = Open(writeFile(t, data))
	require.NoError(t, err)
	require.NotNil(t, f.Section(".debug_info"))
	require.NoError(t, f.Close())
}

func TestNewFile_ExtendedSectionNumberingIsRefused(t *testing.T) {
	data := elfcaptest.Build()
	data[0x3C], data[0x3D] = 0, 0
	_, err := NewFile(bytes.NewReader(data), int64(len(data)))
	require.ErrorContains(t, err, "extended section numbering")
}

func TestNewFile_TheParserReadsTheHeadersTheScanChecked(t *testing.T) {
	data := elfcaptest.Build(elfcaptest.Section{Name: ".debug_info", Type: elf.SHT_PROGBITS, Data: []byte("plain")})
	f, err := NewFile(&flipAfterFirstRead{data: data, at: len(data) - 64 + 8}, int64(len(data)))
	require.NoError(t, err)
	require.Zero(t, f.Section(".debug_info").Flags&elf.SHF_COMPRESSED)
}

type flipAfterFirstRead struct {
	data  []byte
	at    int
	reads int
}

func (f *flipAfterFirstRead) ReadAt(p []byte, off int64) (int, error) {
	f.reads++
	if f.reads == 4 {
		f.data[f.at+1] |= byte(elf.SHF_COMPRESSED >> 8)
	}
	return bytes.NewReader(f.data).ReadAt(p, off)
}
