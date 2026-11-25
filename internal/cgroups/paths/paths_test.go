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

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPath(t *testing.T) {
	t.Setenv("HOST_MOUNT_PREFIX", "")
	tests := []struct {
		name     string
		segments []string
		want     string
	}{
		{
			name:     "no-segments-returns-rootfs-default",
			segments: []string{},
			want:     RootfsDefaultPath,
		},
		{
			name:     "single-segment-huatuo-dev",
			segments: []string{"huatuo-dev"},
			want:     filepath.Join(RootfsDefaultPath, "huatuo-dev"),
		},
		{
			name:     "two-segments-huatuo-region-memory",
			segments: []string{"huatuo-region", "memory"},
			want:     filepath.Join(RootfsDefaultPath, "huatuo-region", "memory"),
		},
		{
			name:     "nested-path-huatuo-dev-cpu-stat",
			segments: []string{"huatuo-dev", "cpu", "cpu.stat"},
			want:     filepath.Join(RootfsDefaultPath, "huatuo-dev", "cpu", "cpu.stat"),
		},
		{
			// filepath.Join cleans ".." — the result must not escape rootfs root.
			name:     "dotdot-segment-is-cleaned",
			segments: []string{"huatuo-dev", "..", "huatuo-region"},
			want:     filepath.Join(RootfsDefaultPath, "huatuo-dev", "..", "huatuo-region"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Path(tc.segments...); got != tc.want {
				t.Errorf("Path(%v): got %q, want %q", tc.segments, got, tc.want)
			}
		})
	}
}

// TestRootfsDefaultPath verifies the package-level default points to the
// standard cgroup v2 mount location expected by the HuaTuo runtime.
func TestRootfsDefaultPath(t *testing.T) {
	const wantCgroupV2Root = "/sys/fs/cgroup"
	if RootfsDefaultPath != wantCgroupV2Root {
		t.Errorf("RootfsDefaultPath: got %q, want %q", RootfsDefaultPath, wantCgroupV2Root)
	}
}

func TestResolveHostCgroupRootCachesAndNormalizesPrefix(t *testing.T) {
	t.Cleanup(func() {
		hostRootMu.Lock()
		cachedPrefix = ""
		cachedHostRoot = ""
		cachedChecked = false
		hostRootMu.Unlock()
	})
	host := t.TempDir()
	want := filepath.Join(host, "sys", "fs", "cgroup")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveHostCgroupRoot("//" + host + "//"); got != want {
		t.Fatalf("resolveHostCgroupRoot(messy host) = %q, want %q", got, want)
	}
	// Second call hits the cache for the same cleaned prefix.
	if got := resolveHostCgroupRoot(host); got != want {
		t.Fatalf("resolveHostCgroupRoot(cached host) = %q, want %q", got, want)
	}
	// Switching to a prefix without /sys/fs/cgroup invalidates the cached prefix.
	emptyHost := t.TempDir()
	if got := resolveHostCgroupRoot(emptyHost); got != "" {
		t.Fatalf("resolveHostCgroupRoot(emptyHost) = %q, want empty", got)
	}
	for _, invalid := range []string{"", "/", "relative/prefix"} {
		if got := resolveHostCgroupRoot(invalid); got != "" {
			t.Fatalf("resolveHostCgroupRoot(%q) = %q, want empty", invalid, got)
		}
	}

	// Verify private-cgroupns host root eligibility across all 3 deployment modes:
	// (a) HOST_MOUNT_PREFIX mounted; (b) direct host /sys bind mount (SameFile); (c) isolated private cgroupfs.
	if !hasHostCgroupRoot(defaultCgroupRoot, true, host, func() bool { return false }) {
		t.Fatal("expected true when HOST_MOUNT_PREFIX contains /sys/fs/cgroup")
	}
	if !hasHostCgroupRoot(defaultCgroupRoot, true, "", func() bool { return true }) {
		t.Fatal("expected true when /sys/fs/cgroup matches /proc/1/root/sys/fs/cgroup without HOST_MOUNT_PREFIX")
	}
	if hasHostCgroupRoot(defaultCgroupRoot, true, "", func() bool { return false }) {
		t.Fatal("expected false when in private cgroupns without HOST_MOUNT_PREFIX or host /sys bind mount")
	}
	sameDir := t.TempDir()
	otherDir := t.TempDir()
	if !sameFileByPath(sameDir, sameDir) {
		t.Fatal("sameFileByPath on identical directory returned false")
	}
	if sameFileByPath(sameDir, otherDir) {
		t.Fatal("sameFileByPath on distinct directories returned true")
	}
}
