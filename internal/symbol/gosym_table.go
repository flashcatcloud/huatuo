package symbol

import (
	"debug/elf"
	"debug/gosym"
	"fmt"

	"huatuo-bamai/internal/log"
)

type goSymTable struct {
	table *gosym.Table
}

func newGoSymTable(f *elf.File) (*goSymTable, error) {
	pclnSec := f.Section(".gopclntab")
	textSec := f.Section(".text")
	if pclnSec == nil || textSec == nil {
		return nil, fmt.Errorf("not a Go binary")
	}
	pcln, err := pclnSec.Data()
	if err != nil {
		return nil, err
	}
	table, err := gosym.NewTable(nil, gosym.NewLineTable(pcln, textSec.Addr))
	if err != nil {
		return nil, err
	}
	if len(table.Funcs) == 0 {
		return nil, fmt.Errorf("empty Go symbol table")
	}
	return &goSymTable{table: table}, nil
}

func (t *goSymTable) resolve(addr uint64) string {
	if t == nil || t.table == nil {
		return ""
	}
	fn := t.table.PCToFunc(addr)
	if fn == nil {
		return ""
	}
	return fn.Name
}

func loadGoSymTable(f *elf.File, path string) *goSymTable {
	t, err := newGoSymTable(f)
	if err != nil {
		return nil
	}
	log.Infof("symbol: gopclntab loaded %d funcs from %s", len(t.table.Funcs), path)
	return t
}
