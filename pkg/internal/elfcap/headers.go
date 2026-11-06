// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package elfcap // import "go.opentelemetry.io/obi/pkg/internal/elfcap"

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cilium/ebpf/link"
)

// MaxNameTableBytes is the most the section name table of a file may hold.
const MaxNameTableBytes = 1 << 20

// shnXindex marks a section name table index held in the first section header.
const shnXindex = 0xffff

type pin struct {
	off  int64
	data []byte
}

type rawSection struct {
	name, flags, offset, size uint64
	typ                       uint32
}

// CheckHeaders refuses, from the raw headers and before the ELF parser reads the file, extended section numbering, a compressed or oversized section name table, and a compressed section other than non-allocated debug information.
func CheckHeaders(r io.ReaderAt, size int64) error {
	_, err := scanHeaders(r, size)
	return err
}

// NewFile parses an ELF file of the given size after CheckHeaders and CheckLayout, the parser reading the file and section headers CheckHeaders read.
func NewFile(r io.ReaderAt, size int64) (*elf.File, error) {
	pins, err := scanHeaders(r, size)
	if err != nil {
		return nil, err
	}
	f, err := elf.NewFile(pinnedReader{r: r, pins: pins})
	if err != nil {
		return nil, err
	}
	if err := CheckLayout(f); err != nil {
		return nil, err
	}
	return f, nil
}

// Open opens the ELF file at path once, checks it with CheckHeaders and parses it through the same open file, then checks it with CheckLayout.
func Open(path string) (*elf.File, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	st, err := fd.Stat()
	if err != nil {
		return nil, err
	}
	if err := CheckHeaders(fd, st.Size()); err != nil {
		return nil, err
	}
	f, err := elf.Open(fmt.Sprintf("/proc/self/fd/%d", fd.Fd()))
	if err != nil {
		return nil, err
	}
	if err := CheckLayout(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// scanHeaders checks the raw headers and returns the file and section headers it read.
func scanHeaders(r io.ReaderAt, size int64) ([]pin, error) {
	var ident [elf.EI_NIDENT]byte
	if _, err := r.ReadAt(ident[:], 0); err != nil {
		return nil, fmt.Errorf("read ELF identification: %w", err)
	}
	if !bytes.Equal(ident[:4], []byte(elf.ELFMAG)) {
		return nil, errors.New("not an ELF file")
	}
	var order binary.ByteOrder
	switch elf.Data(ident[elf.EI_DATA]) {
	case elf.ELFDATA2LSB:
		order = binary.LittleEndian
	case elf.ELFDATA2MSB:
		order = binary.BigEndian
	default:
		return nil, errors.New("unknown ELF data encoding")
	}
	wide := elf.Class(ident[elf.EI_CLASS]) == elf.ELFCLASS64
	if !wide && elf.Class(ident[elf.EI_CLASS]) != elf.ELFCLASS32 {
		return nil, errors.New("unknown ELF class")
	}

	headerLen, entryLen := 52, uint64(40)
	if wide {
		headerLen, entryLen = 64, 64
	}
	header := make([]byte, headerLen)
	if _, err := r.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("read ELF header: %w", err)
	}
	var shoff uint64
	var shentsize, shnum, shstrndx uint16
	if wide {
		shoff = order.Uint64(header[0x28:])
		shentsize, shnum, shstrndx = order.Uint16(header[0x3A:]), order.Uint16(header[0x3C:]), order.Uint16(header[0x3E:])
	} else {
		shoff = uint64(order.Uint32(header[0x20:]))
		shentsize, shnum, shstrndx = order.Uint16(header[0x2E:]), order.Uint16(header[0x30:]), order.Uint16(header[0x32:])
	}
	pins := []pin{{off: 0, data: header}}
	if shnum == 0 {
		if shoff != 0 {
			return nil, errors.New("extended section numbering")
		}
		return pins, nil
	}
	if shstrndx == shnXindex {
		return nil, errors.New("extended section name table index")
	}
	if uint64(shentsize) != entryLen || shoff > uint64(size) || uint64(shnum)*entryLen > uint64(size)-shoff {
		return nil, errors.New("section headers outside the file")
	}
	table := make([]byte, uint64(shnum)*entryLen)
	if _, err := r.ReadAt(table, int64(shoff)); err != nil {
		return nil, fmt.Errorf("read section headers: %w", err)
	}
	pins = append(pins, pin{off: int64(shoff), data: table})

	sections := make([]rawSection, shnum)
	for i := range sections {
		e := table[uint64(i)*entryLen:]
		if wide {
			sections[i] = rawSection{name: uint64(order.Uint32(e)), typ: order.Uint32(e[4:]), flags: order.Uint64(e[8:]), offset: order.Uint64(e[0x18:]), size: order.Uint64(e[0x20:])}
		} else {
			sections[i] = rawSection{name: uint64(order.Uint32(e)), typ: order.Uint32(e[4:]), flags: uint64(order.Uint32(e[8:])), offset: uint64(order.Uint32(e[0x10:])), size: uint64(order.Uint32(e[0x14:]))}
		}
	}

	var names []byte
	if shstrndx != uint16(elf.SHN_UNDEF) && shstrndx < shnum {
		s := sections[shstrndx]
		if elf.SectionFlag(s.flags)&elf.SHF_COMPRESSED != 0 {
			return nil, errors.New("compressed section name table")
		}
		if elf.SectionType(s.typ) != elf.SHT_NOBITS {
			if s.size > MaxNameTableBytes || s.offset > uint64(size) || s.size > uint64(size)-s.offset {
				return nil, fmt.Errorf("section name table of %d bytes", s.size)
			}
			names = make([]byte, s.size)
			if _, err := r.ReadAt(names, int64(s.offset)); err != nil {
				return nil, fmt.Errorf("read section name table: %w", err)
			}
		}
	}

	for _, s := range sections {
		name := sectionName(names, s.name)
		if !rawCompressed(r, size, s, name) {
			continue
		}
		if !debugInformation(name, elf.SectionType(s.typ), elf.SectionFlag(s.flags)) {
			return nil, fmt.Errorf("compressed section %q is not debug information", name)
		}
	}
	return pins, nil
}

// sectionName reads the name at off in the section name table, empty when outside it.
func sectionName(names []byte, off uint64) string {
	if off >= uint64(len(names)) {
		return ""
	}
	rest := names[off:]
	if end := bytes.IndexByte(rest, 0); end >= 0 {
		rest = rest[:end]
	}
	return string(rest)
}

// rawCompressed reports whether a section is flagged compressed or is a .zdebug section opening with the ZLIB magic.
func rawCompressed(r io.ReaderAt, size int64, s rawSection, name string) bool {
	if elf.SectionType(s.typ) == elf.SHT_NOBITS {
		return false
	}
	if elf.SectionFlag(s.flags)&elf.SHF_COMPRESSED != 0 {
		return true
	}
	if !strings.HasPrefix(name, ".zdebug") || s.size < zlibHeaderLen || s.offset > uint64(size)-zlibHeaderLen {
		return false
	}
	var magic [4]byte
	if _, err := r.ReadAt(magic[:], int64(s.offset)); err != nil {
		return false
	}
	return string(magic[:]) == "ZLIB"
}

// debugInformation reports whether a section may be compressed: non-allocated, non-writable, non-executable debug information.
func debugInformation(name string, typ elf.SectionType, flags elf.SectionFlag) bool {
	debugName := strings.HasPrefix(name, ".debug_") || strings.HasPrefix(name, ".zdebug_")
	return debugName && typ == elf.SHT_PROGBITS && flags&(elf.SHF_ALLOC|elf.SHF_WRITE|elf.SHF_EXECINSTR) == 0
}

type pinnedReader struct {
	r    io.ReaderAt
	pins []pin
}

// ReadAt reads from the file, the pinned ranges answered from what the header scan read.
func (p pinnedReader) ReadAt(b []byte, off int64) (int, error) {
	n, err := p.r.ReadAt(b, off)
	for _, pn := range p.pins {
		lo, hi := max(off, pn.off), min(off+int64(n), pn.off+int64(len(pn.data)))
		if lo < hi {
			copy(b[lo-off:hi-off], pn.data[lo-pn.off:hi-pn.off])
		}
	}
	return n, err
}

// OpenExecutable opens the ELF file at path for uprobes after Open accepts it, so the symbol reads of the returned executable parse a file the bounds passed.
func OpenExecutable(path string) (*link.Executable, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return link.OpenExecutable(path)
}
