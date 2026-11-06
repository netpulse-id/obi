// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package elfcap bounds what the compressed sections of an untrusted ELF file may decompress to.
package elfcap // import "go.opentelemetry.io/obi/pkg/internal/elfcap"

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"strings"
)

const (
	// MaxSectionBytes is the most one compressed section may declare once decompressed.
	MaxSectionBytes = 64 << 20
	// MaxTotalBytes is the most the compressed sections of one file may declare together.
	MaxTotalBytes = 256 << 20
)

// zlibHeaderLen is the length of the header of a .zdebug section: the magic ZLIB and the big endian decompressed size.
const zlibHeaderLen = 12

// DeclaredSize returns the size a compressed section declares it decompresses to, and whether the section is compressed.
func DeclaredSize(s *elf.Section) (uint64, bool) {
	if s.Type == elf.SHT_NOBITS {
		return 0, false
	}
	if s.Flags&elf.SHF_COMPRESSED != 0 {
		return s.Size, true
	}
	if !strings.HasPrefix(s.Name, ".zdebug") || s.ReaderAt == nil {
		return 0, false
	}
	var hdr [zlibHeaderLen]byte
	if n, _ := s.ReadAt(hdr[:], 0); n != zlibHeaderLen || string(hdr[:4]) != "ZLIB" {
		return 0, false
	}
	return binary.BigEndian.Uint64(hdr[4:]), true
}

// CheckLayout refuses a file holding a compressed section other than non-allocated debug information.
func CheckLayout(f *elf.File) error {
	for _, s := range f.Sections {
		if _, compressed := DeclaredSize(s); !compressed {
			continue
		}
		if !debugInformation(s.Name, s.Type, s.Flags) {
			return fmt.Errorf("compressed section %q is not debug information", s.Name)
		}
	}
	return nil
}

// CheckDebug refuses a file whose compressed sections declare more than MaxSectionBytes each or MaxTotalBytes together.
func CheckDebug(f *elf.File) error {
	var total uint64
	for _, s := range f.Sections {
		size, compressed := DeclaredSize(s)
		if !compressed {
			continue
		}
		if size > MaxSectionBytes {
			return fmt.Errorf("compressed section %q declares %d bytes, over %d", s.Name, size, MaxSectionBytes)
		}
		total += size
		if total > MaxTotalBytes {
			return fmt.Errorf("compressed sections declare over %d bytes together", MaxTotalBytes)
		}
	}
	return nil
}
