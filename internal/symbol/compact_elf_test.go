package symbol

import (
	"debug/elf"
	"io"
	"os"
	"testing"
)

type countReads struct {
	io.ReaderAt
	calls int
}

func (r *countReads) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	return r.ReaderAt.ReadAt(p, off)
}

func TestCompactELFMatchesSymbolsAndCachesNames(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := elf.Open(path)
	if err != nil {
		t.Skip("requires ELF test executable")
	}
	reference := elfSymbols(f)
	index, ok := newELFIndex(f).(*compactELF)
	if !ok {
		t.Skip("requires ELF64")
	}
	defer f.Close()
	if len(reference) == 0 {
		t.Skip("test executable is stripped")
	}
	if len(index.entries) != len(reference) {
		t.Fatalf("entries = %d, want %d", len(index.entries), len(reference))
	}
	var reads []*countReads
	for _, table := range index.tables {
		if table != nil {
			r := &countReads{ReaderAt: table.ReaderAt}
			table.ReaderAt = r
			reads = append(reads, r)
		}
	}
	verify := func() {
		for i := 0; i < len(reference); i += max(1, len(reference)/1024) {
			for _, pc := range []uint64{reference[i].Addr, reference[i].Addr + 1} {
				if got, want := index.resolve(pc), reference.resolve(pc); got != want {
					t.Fatalf("PC %#x = %q, want %q", pc, got, want)
				}
			}
		}
	}
	verify()
	for _, r := range reads {
		r.calls = 0
	}
	verify()
	for _, r := range reads {
		if r.calls != 0 {
			t.Fatalf("repeated lookups read the string table %d times", r.calls)
		}
	}
}
