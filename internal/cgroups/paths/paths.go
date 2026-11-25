// Copyright 2025, 2026 The HuaTuo Authors
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
	"strings"
	"sync"
)

const defaultCgroupRoot = "/sys/fs/cgroup"

var (
	RootfsDefaultPath = defaultCgroupRoot

	inPrivateCgroupNS = sync.OnceValue(func() bool {
		selfNS, err := os.Readlink("/proc/self/ns/cgroup")
		if err != nil {
			return false
		}
		hostNS, err := os.Readlink("/proc/1/ns/cgroup")
		return err == nil && selfNS != hostNS
	})

	localRootMatchesHost = sync.OnceValue(func() bool {
		return sameFileByPath(defaultCgroupRoot, "/proc/1/root"+defaultCgroupRoot)
	})

	hostRootMu     sync.Mutex
	cachedPrefix   string
	cachedHostRoot string
	cachedChecked  bool
)

func sameFileByPath(a, b string) bool {
	fiA, err := os.Stat(a)
	if err != nil {
		return false
	}
	fiB, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(fiA, fiB)
}

// InPrivateCgroupNS reports whether this process runs in a non-root cgroup namespace.
func InPrivateCgroupNS() bool {
	return inPrivateCgroupNS()
}

func resolveHostCgroupRoot(rawPrefix string) string {
	rawPrefix = strings.TrimSpace(rawPrefix)
	if rawPrefix == "" || !filepath.IsAbs(rawPrefix) {
		return ""
	}
	prefix := filepath.Clean(rawPrefix)
	if prefix == "/" {
		return ""
	}
	hostRootMu.Lock()
	defer hostRootMu.Unlock()
	if cachedChecked && cachedPrefix == prefix {
		return cachedHostRoot
	}
	candidate := filepath.Join(prefix, defaultCgroupRoot)
	resolved := ""
	if _, err := os.Stat(candidate); err == nil {
		resolved = candidate
	}
	cachedPrefix = prefix
	cachedHostRoot = resolved
	cachedChecked = true
	return resolved
}

// HasHostCgroupRoot reports whether host-namespace cgroup paths can be resolved
// on the filesystem (either via HOST_MOUNT_PREFIX or a direct host /sys bind mount).
func HasHostCgroupRoot() bool {
	return hasHostCgroupRoot(RootfsDefaultPath, InPrivateCgroupNS(), os.Getenv("HOST_MOUNT_PREFIX"), localRootMatchesHost)
}

func hasHostCgroupRoot(rootfs string, privateNS bool, rawPrefix string, localMatchesHost func() bool) bool {
	if rootfs != defaultCgroupRoot || !privateNS {
		return true
	}
	if resolveHostCgroupRoot(rawPrefix) != "" {
		return true
	}
	return localMatchesHost()
}

// Root returns the host cgroupfs root, falling back to HOST_MOUNT_PREFIX when
// running inside a private cgroup namespace.
func Root() string {
	if RootfsDefaultPath != defaultCgroupRoot || !InPrivateCgroupNS() {
		return RootfsDefaultPath
	}
	if hostRoot := resolveHostCgroupRoot(os.Getenv("HOST_MOUNT_PREFIX")); hostRoot != "" {
		return hostRoot
	}
	return RootfsDefaultPath
}

func Path(path ...string) string {
	root := []string{Root()}

	return filepath.Join(append(root, path...)...)
}
