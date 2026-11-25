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

package cgroups

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/ccfos/huatuo/internal/cgroups/paths"
	"github.com/ccfos/huatuo/internal/procfs"
)

// ProcessPaths contains the cgroup membership paths reported for a process.
type ProcessPaths struct {
	Unified     string
	Controllers map[string]string
}

// PathForMemory returns the memory-controller membership, including on hybrid hosts.
func (p *ProcessPaths) PathForMemory() (string, error) {
	if p == nil {
		return "", fmt.Errorf("nil process paths")
	}
	if path := p.Controllers["memory"]; path != "" {
		return path, nil
	}
	if p.Unified != "" {
		return p.Unified, nil
	}
	return "", fmt.Errorf("process memory cgroup path not found")
}

// PathsForPID reads the kernel cgroup membership of pid.
func PathsForPID(pid int) (*ProcessPaths, error) {
	if pid <= 0 {
		return nil, fmt.Errorf("invalid pid %d", pid)
	}

	filePath := procfs.Path(strconv.Itoa(pid), "cgroup")
	var reader io.Reader
	if paths.InPrivateCgroupNS() {
		if !paths.HasHostCgroupRoot() {
			return nil, fmt.Errorf("private cgroup namespace requires host cgroupfs mounted at /sys/fs/cgroup or under HOST_MOUNT_PREFIX")
		}
		data, err := readInHostCgroupNS(filePath)
		if err != nil {
			return nil, fmt.Errorf("read pid %d cgroup in host namespace: %w", pid, err)
		}
		reader = bytes.NewReader(data)
	} else {
		file, err := os.Open(filePath)
		if err != nil {
			return nil, fmt.Errorf("open pid %d cgroup: %w", pid, err)
		}
		defer file.Close()
		reader = file
	}

	parsed, err := parseProcessPaths(reader)
	if err != nil {
		return nil, fmt.Errorf("parse pid %d cgroup: %w", pid, err)
	}
	return parsed, nil
}

// readInHostCgroupNS reads /proc/<pid>/cgroup from PID 1's cgroup namespace so
// the kernel formats paths relative to the host cgroup root instead of /../..
func readInHostCgroupNS(filePath string) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		origNS, origErr := os.Open("/proc/thread-self/ns/cgroup")
		if origErr == nil {
			defer origNS.Close()
		}
		nsFile, err := os.Open("/proc/1/ns/cgroup")
		if err != nil {
			runtime.UnlockOSThread()
			ch <- result{err: err}
			return
		}
		defer nsFile.Close()
		if err := unix.Setns(int(nsFile.Fd()), unix.CLONE_NEWCGROUP); err != nil {
			runtime.UnlockOSThread()
			ch <- result{err: err}
			return
		}
		data, err := os.ReadFile(filePath)
		// Restore the original cgroup namespace so the OS thread can be reused;
		// if restoration is unavailable or fails, leave the thread locked so the
		// Go runtime discards it cleanly when this goroutine exits.
		if origErr == nil && unix.Setns(int(origNS.Fd()), unix.CLONE_NEWCGROUP) == nil {
			runtime.UnlockOSThread()
		}
		ch <- result{data: data, err: err}
	}()
	res := <-ch
	return res.data, res.err
}

// PathForProcesses returns the path used to read cgroup.procs on this host.
func (p *ProcessPaths) PathForProcesses() (string, error) {
	if p == nil {
		return "", fmt.Errorf("nil process paths")
	}
	if p.Unified != "" {
		return p.Unified, nil
	}
	for _, controller := range []string{"cpu", "cpuacct", "pids"} {
		if value := p.Controllers[controller]; value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf("process cgroup path not found")
}

func parseProcessPaths(reader io.Reader) (*ProcessPaths, error) {
	result := &ProcessPaths{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		_, entry, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("invalid cgroup entry %q", line)
		}
		controllers, membershipPath, found := strings.Cut(entry, ":")
		if !found || membershipPath == "" {
			return nil, fmt.Errorf("invalid cgroup entry %q", line)
		}

		trimmed := strings.TrimPrefix(membershipPath, "/")
		if trimmed == ".." || strings.HasPrefix(trimmed, "../") {
			return nil, fmt.Errorf("cgroup path %q escapes current cgroup namespace", membershipPath)
		}
		cgroupPath := path.Clean("/" + trimmed)
		if controllers == "" {
			result.Unified = cgroupPath
			continue
		}
		if result.Controllers == nil {
			result.Controllers = make(map[string]string)
		}
		for _, controller := range strings.Split(controllers, ",") {
			if controller != "" {
				result.Controllers[controller] = cgroupPath
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan cgroup: %w", err)
	}
	if result.Unified == "" && len(result.Controllers) == 0 {
		return nil, fmt.Errorf("empty cgroup membership")
	}
	return result, nil
}
