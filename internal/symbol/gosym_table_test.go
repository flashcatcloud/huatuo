package symbol

import (
	"debug/elf"
	"os"
	"testing"
)

func TestGoSymTableCurrentBinary(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	f, err := elf.Open(path)
	if err != nil {
		t.Fatalf("elf.Open: %v", err)
	}
	defer f.Close()

	table, err := newGoSymTable(f)
	if err != nil {
		t.Fatalf("newGoSymTable: %v", err)
	}
	text := f.Section(".text")
	if text == nil {
		t.Fatal("missing .text")
	}
	found := false
	for _, fn := range table.table.Funcs {
		if fn.Entry < text.Addr || fn.Entry >= text.Addr+text.Size {
			continue
		}
		if got := table.resolve(fn.Entry + 1); got == fn.Name {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("PCToFunc did not resolve any in-range function entry")
	}
}
