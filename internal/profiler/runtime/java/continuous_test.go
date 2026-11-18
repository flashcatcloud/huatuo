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

package java

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ccfos/huatuo/internal/profiler"
)

func TestCollapsedFileCollectorConsumesEachFileOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile-0.collapsed")
	if err := os.WriteFile(path, []byte("first 17\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	samples, c := newTestCollapsedFileCollector(t, []int{1234}, map[int]string{1234: filepath.Join(dir, "profile-*.collapsed")})
	for range 3 {
		if err := c.scanOutputFiles(); err != nil {
			t.Fatal(err)
		}
	}
	if len(*samples) != 1 || (*samples)[0].Output != "first 17\n" {
		t.Fatalf("samples: %v", *samples)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("recovery file removed: %v", err)
	}
}

func TestCollapsedFileCollectorCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, marker            string
		sequences               []int
		wantComplete, wantError bool
	}{
		{"missing marker", "", []int{0}, false, false},
		{"complete", "window-completion-v1\n1\n", []int{0, 1}, true, false},
		{"empty window", "window-completion-v1\n0\n", []int{0}, true, false},
		{"gap", "window-completion-v1\n2\n", []int{0, 2}, false, true},
		{"missing tail", "window-completion-v1\n2\n", []int{0, 1}, false, true},
		{"invalid marker", "window-completion-v1\nx\n", []int{0}, false, true},
		{"invalid protocol", "unknown\n0\n", []int{0}, false, true},
		{"overflow", "window-completion-v1\n18446744073709551615\n", []int{0}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, seq := range tc.sequences {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("profile-%d.collapsed", seq)), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.marker != "" {
				if err := os.WriteFile(filepath.Join(dir, "profile.complete"), []byte(tc.marker), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, c := newTestCollapsedFileCollector(t, []int{1234}, map[int]string{1234: filepath.Join(dir, "profile-*.collapsed")})
			if err := c.scanOutputFiles(); err != nil {
				t.Fatal(err)
			}
			complete, err := c.complete()
			if complete != tc.wantComplete || (err != nil) != tc.wantError {
				t.Fatalf("complete=%v err=%v", complete, err)
			}
		})
	}
}

func TestCollapsedFileCollectorIgnoresUnpublishedWindow(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "profile-0.collapsed.part.123")
	if err := os.WriteFile(part, []byte("work 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	samples, c := newTestCollapsedFileCollector(t, []int{1234}, map[int]string{1234: filepath.Join(dir, "profile-*.collapsed")})
	for range 3 {
		if err := c.scanOutputFiles(); err != nil {
			t.Fatal(err)
		}
	}
	if len(*samples) != 0 {
		t.Fatal("read unpublished window")
	}
	if err := os.Rename(part, filepath.Join(dir, "profile-0.collapsed")); err != nil {
		t.Fatal(err)
	}
	if err := c.scanOutputFiles(); err != nil {
		t.Fatal(err)
	}
	if len(*samples) != 1 {
		t.Fatalf("published window count %d", len(*samples))
	}
}

func TestCollapsedFileSequence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		want      uint64
		wantError bool
	}{
		{
			name: "valid sequence",
			path: "/tmp/huatuo-asprof-session-cpu-1234-63.collapsed",
			want: 63,
		},
		{name: "missing sequence", path: "/tmp/profile-final.collapsed", wantError: true},
		{name: "invalid sequence", path: "/tmp/profile-x.collapsed", wantError: true},
		{name: "large sequence", path: "/tmp/profile-1024.collapsed", want: 1024},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := collapsedFileSequence(tt.path)
			if tt.wantError {
				if err == nil {
					t.Errorf("collapsedFileSequence() error=nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Errorf("collapsedFileSequence() error=%v, want nil", err)
				return
			}
			if got != tt.want {
				t.Errorf("collapsedFileSequence()=%d, want %d", got, tt.want)
			}
		})
	}
}

func BenchmarkCollapsedFileSequence(b *testing.B) {
	path := "/tmp/huatuo-asprof-session-cpu-1234-63.collapsed"
	b.ReportAllocs()
	for range b.N {
		if _, err := collapsedFileSequence(path); err != nil {
			b.Fatal(err)
		}
	}
}

func newTestCollapsedFileCollector(
	t *testing.T,
	pids []int,
	pidsToFilePath map[int]string,
) (*[]profiler.SampleOutput, *collapsedFileCollector) {
	t.Helper()
	if pidsToFilePath == nil {
		pidsToFilePath = make(map[int]string, len(pids))
		for _, pid := range pids {
			pidsToFilePath[pid] = filepath.Join(t.TempDir(), "profile-*.collapsed")
		}
	}
	samples := &[]profiler.SampleOutput{}
	collector := newCollapsedFileCollector(
		pids,
		pidsToFilePath,
		func(sample profiler.SampleOutput) error { *samples = append(*samples, sample); return nil },
	)
	return samples, collector
}

func TestCollapsedFileCollectorRetainsRejectedDelivery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile-0.collapsed")
	if err := os.WriteFile(path, []byte("work 17\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, collector := newTestCollapsedFileCollector(t, []int{1234}, map[int]string{1234: filepath.Join(dir, "profile-*.collapsed")})
	rejected := errors.New("queue full")
	collector.enqueueSample = func(profiler.SampleOutput) error { return rejected }
	if err := collector.scanOutputFiles(); !errors.Is(err, rejected) {
		t.Fatalf("scan: %v", err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "work 17\n" {
		t.Fatalf("rejected window changed: %q %v", data, err)
	}
	accepted := 0
	collector.enqueueSample = func(sample profiler.SampleOutput) error {
		accepted++
		if sample.Output != "work 17\n" {
			t.Fatalf("output: %q", sample.Output)
		}
		return nil
	}
	for range 3 {
		if err := collector.scanOutputFiles(); err != nil {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d times", accepted)
	}
}

func TestCollapsedFileCollectorMultiplePIDs(t *testing.T) {
	paths := map[int]string{}
	for _, pid := range []int{11, 22} {
		dir := t.TempDir()
		paths[pid] = filepath.Join(dir, "profile-*.collapsed")
		if err := os.WriteFile(filepath.Join(dir, "profile-0.collapsed"), []byte("work 17\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "profile.complete"), []byte("window-completion-v1\n0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	samples, c := newTestCollapsedFileCollector(t, []int{11, 22}, paths)
	accepted := c.enqueueSample
	rejected := errors.New("queue full")
	reject := true
	c.enqueueSample = func(sample profiler.SampleOutput) error {
		if sample.PID == 11 && reject {
			return rejected
		}
		return accepted(sample)
	}
	if err := c.scanOutputFiles(); !errors.Is(err, rejected) {
		t.Fatalf("scan: %v", err)
	}
	if len(*samples) != 1 || (*samples)[0].PID != 22 {
		t.Fatalf("other PID not delivered independently: %v", *samples)
	}
	reject = false
	// Completion also handles a marker observed before the next directory scan.
	for range 3 {
		complete, err := c.complete()
		if err != nil || !complete {
			t.Fatalf("complete=%v err=%v", complete, err)
		}
	}
	if len(*samples) != 2 || (*samples)[1].PID != 11 {
		t.Fatalf("retry delivery: %v", *samples)
	}
}
