// Copyright 2026 The HuaTuo Authors
// SPDX-License-Identifier: Apache-2.0

package symbol

import (
	"bytes"
	"debug/elf"
	"io"
	"sort"
)

type elfEntry struct {
	addr, size uint64
	name       uint32
	table      uint8
}

type compactELF struct {
	entries []elfEntry
	tables  [2]*elf.Section

	file  *elf.File
	names map[int]string
}

// Keep the full symbol order (including aliases) but read names only for PCs
// actually sampled. Unsupported layouts retain debug/elf's decoding.
func newELFIndex(f *elf.File) *compactELF {
	if f.Class != elf.ELFCLASS64 {
		return nil
	}
	idx := &compactELF{file: f, names: make(map[int]string)}
	types := [...]elf.SectionType{elf.SHT_DYNSYM, elf.SHT_SYMTAB}
	var capacity uint64
	for i, typ := range types {
		s := f.SectionByType(typ)
		if s == nil {
			continue
		}
		if s.Entsize != 24 || s.Size%24 != 0 || uint64(s.Link) >= uint64(len(f.Sections)) {
			return nil
		}
		names := f.Sections[s.Link]
		if names.Type != elf.SHT_STRTAB || names.Flags&elf.SHF_COMPRESSED != 0 {
			return nil
		}
		idx.tables[i] = names
		if s.Size/24 > (2<<20)-capacity {
			return nil
		}
		capacity += s.Size / 24
	}
	// Bound the initial allocation even for files with misleading section sizes.
	idx.entries = make([]elfEntry, 0, min(capacity, 2<<20))
	var buf [24 * 1024]byte
	for i, typ := range types {
		s := f.SectionByType(typ)
		if s == nil {
			continue
		}
		r := s.Open()
		start := len(idx.entries)
		for remaining := s.Size; remaining > 0; {
			chunk := buf[:min(remaining, uint64(len(buf)))]
			if _, err := io.ReadFull(r, chunk); err != nil {
				idx.entries = idx.entries[:start]
				break
			}
			for j := 0; j < len(chunk); j += 24 {
				b := chunk[j:]
				if remaining == s.Size && j == 0 || elf.ST_TYPE(b[4]) != elf.STT_FUNC {
					continue
				}
				idx.entries = append(idx.entries, elfEntry{addr: f.ByteOrder.Uint64(b[8:16]), size: f.ByteOrder.Uint64(b[16:24]), name: f.ByteOrder.Uint32(b), table: uint8(i)})
			}
			remaining -= uint64(len(chunk))
		}
	}
	sort.Slice(idx.entries, func(i, j int) bool { return idx.entries[i].addr < idx.entries[j].addr })
	return idx
}

func (idx *compactELF) resolve(pc uint64) string {
	i := searchFloorIndex(len(idx.entries), func(i int) bool { return idx.entries[i].addr > pc })
	if i < 0 {
		return ""
	}
	e := idx.entries[i]
	if !symbolCovers(e.addr, e.size, pc) {
		return ""
	}
	if s, ok := idx.names[i]; ok {
		return s
	}
	table := idx.tables[e.table]
	off := uint64(e.name)
	if off >= table.Size {
		return ""
	}
	var buf [256]byte
	var name []byte
	for off < table.Size && len(name) < 1<<20 {
		n, err := table.ReadAt(buf[:min(uint64(len(buf)), table.Size-off)], int64(off))
		if end := bytes.IndexByte(buf[:n], 0); end >= 0 {
			s := string(append(name, buf[:end]...))
			idx.names[i] = s
			return s
		}
		if err != nil || n == 0 {
			return ""
		}
		name = append(name, buf[:n]...)
		off += uint64(n)
	}
	return ""
}
