// Copyright 2026 The HuaTuo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package symbol

import (
	"bufio"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func buildGoFallbackFixture(t testing.TB, pie bool) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	const program = `package main
import ("fmt"; "reflect"; "os")
//go:noinline
func sampled() { fmt.Println("sampled") }
func main() {
 fmt.Printf("%x\n", reflect.ValueOf(sampled).Pointer())
 var b [1]byte
 os.Stdin.Read(b[:])
}
`
	if err := os.WriteFile(source, []byte(program), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture")
	args := []string{"build", "-ldflags=-s -w", "-o", path}
	if pie {
		args = append(args, "-buildmode=pie")
	}
	args = append(args, source)
	if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	return path
}

func TestUsymStrippedGoProcess(t *testing.T) {
	for _, pie := range []bool{false, true} {
		t.Run(fmt.Sprintf("pie=%t", pie), func(t *testing.T) {
			path := buildGoFallbackFixture(t, pie)
			file, err := elf.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if file.SectionByType(elf.SHT_SYMTAB) != nil {
				t.Fatal("fixture is not stripped")
			}
			file.Close()
			cmd := exec.Command(path)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
			var pc uint64
			if _, err := fmt.Fscanf(bufio.NewReader(stdout), "%x\n", &pc); err != nil {
				t.Fatal(err)
			}
			r := NewUsymResolver()
			pid := uint32(cmd.Process.Pid)
			if _, err := r.loadElfCaches(pid); err != nil {
				t.Fatal(err)
			}
			// Exercise the same public stack API used by native profiling, including
			// actual process maps and ASLR relocation for the PIE executable.
			stack := []uint64{pc, pc + 1, 0}
			want := []string{"main.sampled", "main.sampled"}
			for range 2 {
				if got := r.UsymStackStrsReversed(pid, stack, len(stack)); !slices.Equal(got, want) {
					t.Fatalf("frames: got %v, want %v", got, want)
				}
			}
			cache := &r.exeCache[r.processes[pid].cacheKey].symbols
			if cache.goTable == nil {
				t.Fatal("Go fallback table was not loaded")
			}
			table := cache.goTable
			if got := r.resolveAddr(pid, pc+2); got != "main.sampled" {
				t.Fatalf("new PC: %s", got)
			}
			if cache.goTable != table {
				t.Fatal("Go table was reloaded")
			}
			// Invalid addresses must not be attributed to the nearest Go function.
			got, _ := r.resolveELFPCs(path, cache, []uint64{1})
			if got[1] != "" {
				t.Fatalf("invalid PC resolved to %q", got[1])
			}
		})
	}
}

func TestGoFallbackNonGoELF(t *testing.T) {
	r := NewUsymResolver()
	cache := &elfSymbolCache{}
	for range 2 {
		got, err := r.resolveELFPCs("testdata/symbols64.elf", cache, []uint64{1})
		if err != nil {
			t.Fatal(err)
		}
		if got[1] != "" {
			t.Fatalf("invalid PC resolved to %q", got[1])
		}
	}
	if !cache.goTableLoaded || cache.goTable != nil {
		t.Fatal("non-Go ELF should cache the failed lookup")
	}
}

func BenchmarkGoFallbackCachedPC(b *testing.B) {
	path := buildGoFallbackFixture(b, false)
	file, err := elf.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	table, err := ReadGoTable(b.Context(), file)
	file.Close()
	if err != nil {
		b.Fatal(err)
	}
	fn := table.LookupFunc("main.sampled")
	if fn == nil {
		b.Fatal("missing sampled function")
	}
	r := NewUsymResolver()
	cache := &elfSymbolCache{}
	pcs := []uint64{fn.Entry + 1}
	got, _ := r.resolveELFPCs(path, cache, pcs)
	if got[pcs[0]] != "main.sampled" {
		b.Fatal(got)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = r.resolveELFPCs(path, cache, pcs)
	}
}
