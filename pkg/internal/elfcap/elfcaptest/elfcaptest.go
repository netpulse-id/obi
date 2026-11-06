// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package elfcaptest builds small ELF files whose sections declare chosen decompressed sizes.
package elfcaptest // import "go.opentelemetry.io/obi/pkg/internal/elfcap/elfcaptest"

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
)

// Section is one section of a built file.
type Section struct {
	Name  string
	Type  elf.SectionType
	Flags elf.SectionFlag
	Data  []byte
}

// ZstdSection returns the contents of an SHF_COMPRESSED section declaring the given size, holding zstd blocks that repeat one byte up to that size.
func ZstdSection(declared int) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, elf.Chdr64{Type: uint32(elf.COMPRESS_ZSTD), Size: uint64(declared), Addralign: 1})
	b.Write([]byte{0x28, 0xB5, 0x2F, 0xFD, 0x00, 0x38})
	const block = 128 << 10
	for left := declared; left > 0; {
		n := min(left, block)
		left -= n
		last := 0
		if left == 0 {
			last = 1
		}
		h := uint32(last) | 1<<1 | uint32(n)<<3
		b.Write([]byte{byte(h), byte(h >> 8), byte(h >> 16), 'A'})
	}
	return b.Bytes()
}

// ZlibHeader returns the header of a .zdebug section declaring the given size, followed by no stream.
func ZlibHeader(declared uint64) []byte {
	b := []byte("ZLIB\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.BigEndian.PutUint64(b[4:], declared)
	return b
}

// Build returns a 64-bit little endian x86-64 executable holding the given sections after a string table.
func Build(sections ...Section) []byte {
	return BuildWithNameTable(nil, sections...)
}

// BuildWithNameTable returns Build's file with the section name table flagged compressed and holding the given contents, unless they are nil.
func BuildWithNameTable(compressedNames []byte, sections ...Section) []byte {
	const ehsize, shentsize = 64, 64
	shstr := []byte("\x00.shstrtab\x00")
	names := make([]uint32, len(sections))
	for i, s := range sections {
		names[i] = uint32(len(shstr))
		shstr = append(append(shstr, s.Name...), 0)
	}
	var body bytes.Buffer
	offs := make([]int, len(sections))
	for i, s := range sections {
		offs[i] = ehsize + body.Len()
		body.Write(s.Data)
	}
	strOff := ehsize + body.Len()
	var strFlags elf.SectionFlag
	if compressedNames != nil {
		shstr, strFlags = compressedNames, elf.SHF_COMPRESSED
	}
	body.Write(shstr)
	shOff := ehsize + body.Len()

	var f bytes.Buffer
	hdr := elf.Header64{
		Type: uint16(elf.ET_EXEC), Machine: uint16(elf.EM_X86_64), Version: 1,
		Shoff: uint64(shOff), Ehsize: ehsize, Shentsize: shentsize,
		Shnum: uint16(len(sections) + 2), Shstrndx: 1,
	}
	copy(hdr.Ident[:], []byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)})
	_ = binary.Write(&f, binary.LittleEndian, hdr)
	f.Write(body.Bytes())
	_ = binary.Write(&f, binary.LittleEndian, elf.Section64{})
	_ = binary.Write(&f, binary.LittleEndian, elf.Section64{Name: 1, Type: uint32(elf.SHT_STRTAB), Flags: uint64(strFlags), Off: uint64(strOff), Size: uint64(len(shstr)), Addralign: 1})
	for i, s := range sections {
		_ = binary.Write(&f, binary.LittleEndian, elf.Section64{
			Name: names[i], Type: uint32(s.Type), Flags: uint64(s.Flags),
			Off: uint64(offs[i]), Size: uint64(len(s.Data)), Addralign: 1,
		})
	}
	return f.Bytes()
}
