// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package elfcap

import (
	"bytes"
	"debug/elf"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/pkg/internal/elfcap/elfcaptest"
)

func open(t *testing.T, sections ...elfcaptest.Section) *elf.File {
	t.Helper()
	f, err := elf.NewFile(bytes.NewReader(elfcaptest.Build(sections...)))
	require.NoError(t, err)
	return f
}

func debugSection(name string, declared int) elfcaptest.Section {
	return elfcaptest.Section{Name: name, Type: elf.SHT_PROGBITS, Flags: elf.SHF_COMPRESSED, Data: elfcaptest.ZstdSection(declared)}
}

func TestCheckDebug_SectionOverTheBoundIsRefusedBeforeDecompression(t *testing.T) {
	f := open(t, debugSection(".debug_info", 256<<20))
	require.NoError(t, CheckLayout(f))

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	require.ErrorContains(t, CheckDebug(f), ".debug_info")
	runtime.ReadMemStats(&after)
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}

func TestCheckDebug_ZdebugHeaderOverTheBoundIsRefused(t *testing.T) {
	f := open(t, elfcaptest.Section{Name: ".zdebug_info", Type: elf.SHT_PROGBITS, Data: elfcaptest.ZlibHeader(1 << 30)})
	require.NoError(t, CheckLayout(f))
	require.ErrorContains(t, CheckDebug(f), ".zdebug_info")
}

func TestCheckDebug_SectionsOverTheTotalAreRefused(t *testing.T) {
	var sections []elfcaptest.Section
	for _, name := range []string{".debug_info", ".debug_str", ".debug_line", ".debug_abbrev", ".debug_ranges"} {
		sections = append(sections, debugSection(name, 60<<20))
	}
	require.ErrorContains(t, CheckDebug(open(t, sections...)), "together")
}

func TestCheckDebug_SectionsWithinTheBoundsAreRead(t *testing.T) {
	f := open(t, debugSection(".debug_info", 1<<20), elfcaptest.Section{Name: ".zdebug_line", Type: elf.SHT_PROGBITS, Data: elfcaptest.ZlibHeader(1 << 20)})
	require.NoError(t, CheckLayout(f))
	require.NoError(t, CheckDebug(f))

	data, err := f.Section(".debug_info").Data()
	require.NoError(t, err)
	require.Len(t, data, 1<<20)
}

func TestCheckLayout_CompressedSectionOutsideDebugInformationIsRefused(t *testing.T) {
	for _, s := range []elfcaptest.Section{
		{Name: ".gopclntab", Type: elf.SHT_PROGBITS, Flags: elf.SHF_COMPRESSED, Data: elfcaptest.ZstdSection(1 << 10)},
		{Name: ".debug_info", Type: elf.SHT_PROGBITS, Flags: elf.SHF_COMPRESSED | elf.SHF_WRITE, Data: elfcaptest.ZstdSection(1 << 10)},
		{Name: ".debug_info", Type: elf.SHT_RELA, Flags: elf.SHF_COMPRESSED, Data: elfcaptest.ZstdSection(1 << 10)},
		{Name: ".zdebug_data", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_WRITE, Data: elfcaptest.ZlibHeader(1 << 10)},
	} {
		t.Run(s.Name, func(t *testing.T) {
			require.ErrorContains(t, CheckLayout(open(t, s)), "not debug information")
		})
	}
}

func TestCheckLayout_UncompressedSectionsPass(t *testing.T) {
	f := open(t,
		elfcaptest.Section{Name: ".data", Type: elf.SHT_PROGBITS, Flags: elf.SHF_ALLOC | elf.SHF_WRITE, Data: []byte("ZLIB\xff\xff\xff\xff\xff\xff\xff\xff")},
		elfcaptest.Section{Name: ".zdebug_info", Type: elf.SHT_PROGBITS, Data: []byte("plain")},
	)
	require.NoError(t, CheckLayout(f))
	require.NoError(t, CheckDebug(f))
}
