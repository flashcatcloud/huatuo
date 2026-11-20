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

package bpf

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	kprobeOnce   sync.Mutex
	kprobeCache  map[string]struct{}
	kprobeCached bool

	kprobeFunctionFiles = []string{
		"/sys/kernel/tracing/available_filter_functions",
		"/sys/kernel/debug/tracing/available_filter_functions",
	}
	traceFSRoots = []string{
		"/sys/kernel/tracing",
		"/sys/kernel/debug/tracing",
	}
)

func scanKprobeFunctions(r io.Reader, cache map[string]struct{}) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		cache[strings.Fields(line)[0]] = struct{}{}
	}
	return scanner.Err()
}

// hostPrefixed appends each absolute path again under HOST_MOUNT_PREFIX. A
// privileged agent container that sees the host root there (/hostfs) usually
// has its own /sys/kernel/tracing as an empty mountpoint, so the host copy must
// be tried after the local one rather than only when the local path is absent.
func hostPrefixed(paths []string) []string {
	prefix := strings.TrimRight(os.Getenv("HOST_MOUNT_PREFIX"), "/")
	if prefix == "" {
		return paths
	}
	out := make([]string, 0, 2*len(paths))
	out = append(out, paths...)
	for _, p := range paths {
		if filepath.IsAbs(p) {
			out = append(out, filepath.Join(prefix, p))
		}
	}
	return out
}

// TraceFSRoots lists the tracefs roots to probe, local layouts first.
func TraceFSRoots() []string {
	return hostPrefixed(traceFSRoots)
}

// loadKprobeFunctions checks both tracefs layouts because current kernels
// commonly mount tracefs independently while older systems expose it through
// debugfs. A failed read is retried on the next lookup.
func loadKprobeFunctions() {
	cache := make(map[string]struct{})
	readOK := false
	for _, path := range hostPrefixed(kprobeFunctionFiles) {
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		err = scanKprobeFunctions(file, cache)
		_ = file.Close()
		if err == nil {
			readOK = true
		}
	}

	if readOK {
		kprobeCache = cache
		kprobeCached = true
	}
}

// HasKprobeFunction returns whether the given symbol is reported as
// attachable in the kernel's available_filter_functions list.
func HasKprobeFunction(name string) bool {
	kprobeOnce.Lock()
	defer kprobeOnce.Unlock()
	if !kprobeCached {
		loadKprobeFunctions()
	}

	_, ok := kprobeCache[name]
	return ok
}

// TracepointAvailable reports whether tracefs exposes the tracepoint event.
func TracepointAvailable(group, name string) (bool, error) {
	var statErrors []error
	for _, root := range TraceFSRoots() {
		path := filepath.Join(root, "events", group, name, "id")
		_, err := os.Stat(path)
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			statErrors = append(statErrors, fmt.Errorf("check tracepoint %q: %w", path, err))
		}
	}
	return false, errors.Join(statErrors...)
}
