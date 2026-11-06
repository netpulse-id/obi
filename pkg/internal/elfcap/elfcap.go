// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package elfcap bounds what the compressed sections of an untrusted ELF file may decompress to.
package elfcap // import "go.opentelemetry.io/obi/pkg/internal/elfcap"

import (
	"debug/elf"
	"fmt"
	"strings"
)

const (
	// MaxSectionBytes is the most one compressed section may declare once decompressed.
	MaxSectionBytes = 64 << 20
	// MaxTotalBytes is the most the compressed sections of one file may declare together.
	MaxTotalBytes = 256 << 20
)

// zdebugPrefix opens the name of a section the parser inflates when its data opens with the ZLIB magic.
const zdebugPrefix = ".zdebug"

// compressed reports whether the parser may inflate a section: flagged compressed, or named .zdebug.
func compressed(s *elf.Section) bool {
	return s.Type != elf.SHT_NOBITS && (s.Flags&elf.SHF_COMPRESSED != 0 || strings.HasPrefix(s.Name, zdebugPrefix))
}

// CheckLayout refuses a file holding a compressed section other than non-allocated debug information.
func CheckLayout(f *elf.File) error {
	for _, s := range f.Sections {
		if compressed(s) && !debugInformation(s.Name, s.Type, s.Flags) {
			return fmt.Errorf("compressed section %q is not debug information", s.Name)
		}
	}
	return nil
}

// CheckDebug refuses a file holding a .zdebug section, or whose compressed sections declare more than MaxSectionBytes each or MaxTotalBytes together.
func CheckDebug(f *elf.File) error {
	var total uint64
	for _, s := range f.Sections {
		if !compressed(s) {
			continue
		}
		if strings.HasPrefix(s.Name, zdebugPrefix) {
			return fmt.Errorf("section %q is compressed in the .zdebug form", s.Name)
		}
		if s.Size > MaxSectionBytes {
			return fmt.Errorf("compressed section %q declares %d bytes, over %d", s.Name, s.Size, MaxSectionBytes)
		}
		total += s.Size
		if total > MaxTotalBytes {
			return fmt.Errorf("compressed sections declare over %d bytes together", MaxTotalBytes)
		}
	}
	return nil
}
