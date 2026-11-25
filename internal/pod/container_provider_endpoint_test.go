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

package pod

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeEndpointOnHost(t *testing.T) {
	host := t.TempDir()
	sock := "/run/k3s/containerd/containerd.sock"
	if err := os.MkdirAll(filepath.Join(host, filepath.Dir(sock)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, sock), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOST_MOUNT_PREFIX", "")
	if got := runtimeEndpointOnHost("unix://" + sock); got != "unix://"+sock {
		t.Fatalf("without a prefix got %q", got)
	}
	t.Setenv("HOST_MOUNT_PREFIX", host+"/")
	if got, want := runtimeEndpointOnHost("unix://"+sock), "unix://"+filepath.Join(host, sock); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := runtimeEndpointOnHost("unix:///run/missing.sock"); got != "unix:///run/missing.sock" {
		t.Fatalf("missing socket got %q", got)
	}
	if got, want := hostVisiblePath("/run/k3s/containerd"), filepath.Join(host, "/run/k3s/containerd"); got != want {
		t.Fatalf("state dir got %q, want %q", got, want)
	}
	if got := runtimeEndpointOnHost("tcp://127.0.0.1:1234"); got != "tcp://127.0.0.1:1234" {
		t.Fatalf("tcp endpoint got %q", got)
	}
}

func TestContainerInitPIDFallbackFindsK3sStateUnderHostPrefix(t *testing.T) {
	host := t.TempDir()
	id := "b12508996e52dfaf2dad08db7329f470bbe8993dbdbcb1be4f69b1b7c5d0bc25"
	dir := filepath.Join(host, "/run/k3s/containerd/io.containerd.runtime.v2.task/k8s.io", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.pid"), []byte("4242"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_MOUNT_PREFIX", host)

	initMu.Lock()
	saved := currContainerProvider
	currContainerProvider = containerProviderNoop
	initMu.Unlock()
	t.Cleanup(func() {
		initMu.Lock()
		currContainerProvider = saved
		initMu.Unlock()
	})

	pid, err := containerInitPIDByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 4242 {
		t.Fatalf("pid = %d", pid)
	}
}

func TestContainerInitPIDFallbackSkipsEmptyLocalStateDir(t *testing.T) {
	localState := t.TempDir()
	host := t.TempDir()
	id := "b12508996e52dfaf2dad08db7329f470bbe8993dbdbcb1be4f69b1b7c5d0bc25"
	hostTaskDir := filepath.Join(host, localState, "io.containerd.runtime.v2.task", "k8s.io", id)
	if err := os.MkdirAll(hostTaskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostTaskDir, "init.pid"), []byte("5353"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_MOUNT_PREFIX", host)

	oldDirs := containerdStateDirs
	containerdStateDirs = []string{localState}
	t.Cleanup(func() { containerdStateDirs = oldDirs })

	initMu.Lock()
	saved := currContainerProvider
	currContainerProvider = containerProviderNoop
	initMu.Unlock()
	t.Cleanup(func() {
		initMu.Lock()
		currContainerProvider = saved
		initMu.Unlock()
	})

	pid, err := containerInitPIDByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 5353 {
		t.Fatalf("pid = %d, want 5353", pid)
	}
}

func TestContainerInitPIDInitializedProviderSkipsEmptyLocalStateDir(t *testing.T) {
	localState := t.TempDir()
	host := t.TempDir()
	id := "b12508996e52dfaf2dad08db7329f470bbe8993dbdbcb1be4f69b1b7c5d0bc25"
	hostTaskDir := filepath.Join(host, localState, "io.containerd.runtime.v2.task", "k8s.io", id)
	if err := os.MkdirAll(hostTaskDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hostTaskDir, "init.pid"), []byte("6464"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOST_MOUNT_PREFIX", host)

	initMu.Lock()
	savedProvider, savedState := currContainerProvider, containerdStateDir
	currContainerProvider, containerdStateDir = containerProviderContainerd, localState
	initMu.Unlock()
	t.Cleanup(func() {
		initMu.Lock()
		currContainerProvider, containerdStateDir = savedProvider, savedState
		initMu.Unlock()
	})

	for name, resolve := range map[string]func(string) (int, error){"containerInitPid": containerInitPid, "containerInitPIDByID": containerInitPIDByID} {
		pid, err := resolve(id)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if pid != 6464 {
			t.Fatalf("%s: pid = %d, want 6464", name, pid)
		}
	}
}

func TestKubeletConfigFileDefaultReadsUnderHostMountPrefix(t *testing.T) {
	host := t.TempDir()
	local := filepath.Join(t.TempDir(), "kubelet", "config.yaml")
	if err := os.MkdirAll(filepath.Join(host, filepath.Dir(local)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, local), []byte("cgroupDriver: systemd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := kubeletDefaultConfigPath
	kubeletDefaultConfigPath = []string{local}
	t.Cleanup(func() { kubeletDefaultConfigPath = old })

	t.Setenv("HOST_MOUNT_PREFIX", "")
	if _, err := kubeletConfigFileDefault(); err == nil {
		t.Fatal("found a config that only exists under the host prefix")
	}
	t.Setenv("HOST_MOUNT_PREFIX", host)
	config, err := kubeletConfigFileDefault()
	if err != nil {
		t.Fatal(err)
	}
	if config.CgroupDriver != "systemd" {
		t.Fatalf("cgroupDriver = %q, want systemd", config.CgroupDriver)
	}
}
