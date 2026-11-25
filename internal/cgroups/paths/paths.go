// Copyright 2025 The HuaTuo Authors
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
)

const defaultCgroupRoot = "/sys/fs/cgroup"

var RootfsDefaultPath = defaultCgroupRoot

// InPrivateCgroupNS reports whether this process is in a non-root cgroup namespace
// (e.g. Docker 20.10+ on cgroup v2), where /sys/fs/cgroup hides sibling pods.
func InPrivateCgroupNS() bool {
	selfNS, err := os.Readlink("/proc/self/ns/cgroup")
	if err != nil {
		return false
	}
	hostNS, err := os.Readlink("/proc/1/ns/cgroup")
	return err == nil && selfNS != hostNS
}

// Root returns the host cgroupfs root, falling back to HOST_MOUNT_PREFIX when
// running inside a private cgroup namespace.
func Root() string {
	if RootfsDefaultPath != defaultCgroupRoot {
		return RootfsDefaultPath
	}
	prefix := strings.TrimRight(os.Getenv("HOST_MOUNT_PREFIX"), "/")
	if prefix == "" || !InPrivateCgroupNS() {
		return RootfsDefaultPath
	}
	hostRoot := filepath.Join(prefix, RootfsDefaultPath)
	if _, err := os.Stat(hostRoot); err == nil {
		return hostRoot
	}
	return RootfsDefaultPath
}

func Path(path ...string) string {
	root := []string{Root()}

	return filepath.Join(append(root, path...)...)
}
