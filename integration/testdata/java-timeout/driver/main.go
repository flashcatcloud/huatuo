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

// A real runtime controller for integration/testdata/java-timeout/runtime.py.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ccfos/huatuo/internal/profiler"
	javaruntime "github.com/ccfos/huatuo/internal/profiler/runtime/java"
)

func main() {
	pid := flag.Int("pid", 0, "test JVM PID")
	tool := flag.String("tool", "", "patched async-profiler directory")
	ready := flag.String("ready", "", "controller readiness JSON")
	output := flag.String("output", "", "collected windows JSON")
	mode := flag.String("mode", "natural", "natural, stop, or kill")
	flag.Parse()
	if err := run(*pid, *tool, *ready, *output, *mode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(pid int, tool, ready, output, mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := javaruntime.PrepareJavaAgent(pid, tool); err != nil {
		return err
	}
	opt := &javaruntime.AsprofSamplingOption{
		Pids: []int{pid}, ToolPath: tool, OutFilePrefix: "integration",
		BaseArgs: []string{"--libpath", "/tmp/libasyncProfiler.so", "-e", "Counted.hit"},
		Duration: 3 * time.Second, AggrInterval: time.Second,
	}
	paths, err := javaruntime.StartAsprofSampling(ctx, opt)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(paths)
	if err != nil {
		return err
	}
	if err := os.WriteFile(ready, encoded, 0o600); err != nil {
		return err
	}
	if mode == "kill" {
		select {}
	}
	if mode == "stop" {
		time.AfterFunc(2100*time.Millisecond, cancel)
	}
	samples := []profiler.SampleOutput{}
	enqueue := func(data any) error { samples = append(samples, data.(profiler.SampleOutput)); return nil }
	if err := javaruntime.ReadAsprofDataLoopAcknowledged(ctx, opt, paths, enqueue); err != nil {
		return err
	}
	count := len(samples)
	// Repeated finalization must neither enqueue again nor change the files.
	for range 3 {
		if err := javaruntime.ReadAsprofDataLoopAcknowledged(ctx, opt, paths, enqueue); err != nil {
			return err
		}
		if err := javaruntime.StopJavaProfiler(ctx, opt); err != nil {
			return err
		}
	}
	if len(samples) != count {
		return fmt.Errorf("duplicate delivery: %d -> %d", count, len(samples))
	}
	encoded, err = json.Marshal(samples)
	if err != nil {
		return err
	}
	return os.WriteFile(output, encoded, 0o600)
}
