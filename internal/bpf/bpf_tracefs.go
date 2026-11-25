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

// hostPrefixed lists the local paths, then the same paths under HOST_MOUNT_PREFIX.
func hostPrefixed(paths []string) []string {
	return append(append([]string{}, paths...), underHostPrefix(paths)...)
}

func underHostPrefix(paths []string) []string {
	prefix := strings.TrimRight(os.Getenv("HOST_MOUNT_PREFIX"), "/")
	if prefix == "" {
		return nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			out = append(out, filepath.Join(prefix, p))
		}
	}
	return out
}

// loadKprobeFunctions checks both tracefs layouts because current kernels
// commonly mount tracefs independently while older systems expose it through
// debugfs. The host copy lists the same kernel, so it is read only when no
// local layout is readable. A failed read is retried on the next lookup.
func loadKprobeFunctions() {
	cache := make(map[string]struct{})
	readOK := scanKprobeFiles(kprobeFunctionFiles, cache)
	if !readOK {
		readOK = scanKprobeFiles(underHostPrefix(kprobeFunctionFiles), cache)
	}

	if readOK {
		kprobeCache = cache
		kprobeCached = true
	}
}

func scanKprobeFiles(paths []string, cache map[string]struct{}) bool {
	readOK := false
	for _, path := range paths {
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
	return readOK
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
	for _, root := range hostPrefixed(traceFSRoots) {
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
