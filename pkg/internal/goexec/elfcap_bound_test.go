// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package goexec

import (
	"bytes"
	"debug/elf"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/obi/internal/goversion"
	"go.opentelemetry.io/obi/pkg/internal/elfcap/elfcaptest"
)

func compressedDebugBomb(t *testing.T) *elf.File {
	t.Helper()
	raw := elfcaptest.Build(elfcaptest.Section{
		Name: ".debug_info", Type: elf.SHT_PROGBITS, Flags: elf.SHF_COMPRESSED,
		Data: elfcaptest.ZstdSection(256 << 20),
	})
	require.Less(t, len(raw), 16<<10)
	f, err := elf.NewFile(bytes.NewReader(raw))
	require.NoError(t, err)
	return f
}

func allocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestStructMemberOffsets_CompressedDebugBombIsNotDecompressed(t *testing.T) {
	f := compressedDebugBomb(t)
	allocated := allocatedBy(func() { _, _ = structMemberOffsets(f) })
	require.Less(t, allocated, uint64(32<<20))
}

func TestLoadGoRuntimeABI_CompressedDebugBombIsNotDecompressed(t *testing.T) {
	f := compressedDebugBomb(t)
	allocated := allocatedBy(func() { _, _ = loadGoRuntimeABI(f, goversion.Version{}) })
	require.Less(t, allocated, uint64(32<<20))
}
