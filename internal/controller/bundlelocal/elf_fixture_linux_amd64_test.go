//go:build linux && amd64

package bundlelocal

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
)

// syntheticSharedObject is the smallest x86-64 ELF shared object debug/elf
// reads a dynamic section from: a dynamic string table, the DT_NEEDED entries
// and the DT_RPATH or DT_RUNPATH strings given, and the section headers that
// locate them. It is built in memory, so no compiler or linker on the host is
// needed.
func syntheticSharedObject(needed []string, rpath, runpath string) []byte {
	var strtab bytes.Buffer
	strtab.WriteByte(0)
	intern := func(value string) uint64 {
		offset := uint64(strtab.Len())
		strtab.WriteString(value)
		strtab.WriteByte(0)
		return offset
	}
	type entry struct{ tag, value uint64 }
	var dynamic []entry
	for _, name := range needed {
		dynamic = append(dynamic, entry{uint64(elf.DT_NEEDED), intern(name)})
	}
	if rpath != "" {
		dynamic = append(dynamic, entry{uint64(elf.DT_RPATH), intern(rpath)})
	}
	if runpath != "" {
		dynamic = append(dynamic, entry{uint64(elf.DT_RUNPATH), intern(runpath)})
	}
	shstrtab := []byte("\x00.dynstr\x00.dynamic\x00.shstrtab\x00")
	const headerSize, sectionSize = 64, 64
	dynstrOffset := uint64(headerSize)
	dynamicOffset := dynstrOffset + uint64(strtab.Len())
	dynamicOffset += (8 - dynamicOffset%8) % 8
	dynamicSize := uint64(len(dynamic)+1) * 16
	shstrOffset := dynamicOffset + dynamicSize
	sectionsOffset := shstrOffset + uint64(len(shstrtab))
	sectionsOffset += (8 - sectionsOffset%8) % 8

	var out bytes.Buffer
	write := func(value any) { _ = binary.Write(&out, binary.LittleEndian, value) }
	out.Write([]byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT), 0})
	out.Write(make([]byte, 8))
	write(uint16(elf.ET_DYN))
	write(uint16(elf.EM_X86_64))
	write(uint32(elf.EV_CURRENT))
	write(uint64(0))
	write(uint64(0))
	write(sectionsOffset)
	write(uint32(0))
	write(uint16(headerSize))
	write(uint16(56))
	write(uint16(0))
	write(uint16(sectionSize))
	write(uint16(4))
	write(uint16(3))
	out.Write(strtab.Bytes())
	out.Write(make([]byte, int(dynamicOffset)-out.Len()))
	for _, e := range dynamic {
		write(e.tag)
		write(e.value)
	}
	write(uint64(elf.DT_NULL))
	write(uint64(0))
	out.Write(shstrtab)
	out.Write(make([]byte, int(sectionsOffset)-out.Len()))
	section := func(name uint32, kind elf.SectionType, offset, size, link, entsize uint64) {
		write(name)
		write(uint32(kind))
		write(uint64(0))
		write(uint64(0))
		write(offset)
		write(size)
		write(uint32(link))
		write(uint32(0))
		write(uint64(1))
		write(entsize)
	}
	section(0, elf.SHT_NULL, 0, 0, 0, 0)
	section(1, elf.SHT_STRTAB, dynstrOffset, uint64(strtab.Len()), 0, 0)
	section(9, elf.SHT_DYNAMIC, dynamicOffset, dynamicSize, 1, 16)
	section(18, elf.SHT_STRTAB, shstrOffset, uint64(len(shstrtab)), 0, 0)
	return out.Bytes()
}
