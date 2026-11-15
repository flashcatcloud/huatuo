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

package handlers

import (
	"testing"
)

func TestHostCPURequest(t *testing.T) {
	for _, lang := range []string{"java", "python", "go", "c++"} {
		args := []string{"-t", "cpu", "-l", lang, "--pid", "42"}
		window := "10"
		if lang == "java" || lang == "python" {
			args = append(args, "--tool-path", "/opt/huatuo")
		} else {
			args = append(args, "--thread-group")
		}
		if lang == "python" {
			window = "30"
		}
		args = append(args, "--tracer-id", "cp-1", "--duration", "30", "--aggr-interval", window, "--max-concurrent-procs", "10", "--output-format", "remote", "--output-storage", "/var/run/huatuo-toolstream.sock")
		req := NewTaskReq{RequestID: "cp-1", TracerName: "profiler", Timeout: 50, DataType: "db-json", TracerArgs: args}
		exe := "/opt/huatuo/bin/huatuo-bamai"
		if _, err := parseHostTask(&req, exe); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		for _, mutate := range []func(*NewTaskReq){
			func(r *NewTaskReq) { r.TracerName = "perf" },
			func(r *NewTaskReq) { r.RequestID = "different" },
			func(r *NewTaskReq) { r.TracerArgs[1] = "memory" },
			func(r *NewTaskReq) { r.TracerArgs[5] = "0" },
			func(r *NewTaskReq) { r.TracerArgs = append(r.TracerArgs, "--duration", "99") },
			func(r *NewTaskReq) { r.TracerArgs[len(r.TracerArgs)-1] = "/tmp/other.sock" },
		} {
			bad := req
			bad.TracerArgs = append([]string(nil), args...)
			mutate(&bad)
			if _, err := parseHostTask(&bad, exe); err == nil {
				t.Fatalf("accepted unexpected request %v", bad)
			}
		}
	}
}

func TestParseHostNativeModes(t *testing.T) {
	for _, tc := range []struct {
		flags       []string
		mode, phase string
	}{
		{[]string{"memory", "--memory-mode", "physical_alloc"}, "physical_alloc", ""},
		{[]string{"cpu", "--cpu-mode", "offcpu", "--offcpu-phase", "blocked"}, "offcpu", "blocked"},
		{[]string{"cpu", "--cpu-mode", "offcpu", "--offcpu-phase", "runqueue"}, "offcpu", "runqueue"},
	} {
		args := append([]string{"-t"}, tc.flags...)
		args = append(args, "-l", "c++", "--pid", "42", "--thread-group", "--tracer-id", "test", "--duration", "30", "--aggr-interval", "10", "--max-concurrent-procs", "10", "--output-format", "remote", "--output-storage", "/var/run/huatuo-toolstream.sock")
		req, err := parseHostTask(&NewTaskReq{RequestID: "test", TracerName: "profiler", DataType: "db-json", Timeout: 50, TracerArgs: args}, "")
		if err != nil || req.HostPID != 42 || string(req.Spec.Mode) != tc.mode || string(req.OffCPUPhase) != tc.phase {
			t.Fatalf("%v: request=%+v err=%v", tc.flags, req, err)
		}
	}
}
