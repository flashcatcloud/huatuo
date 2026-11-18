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
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ccfos/huatuo/internal/profiler"
	profilerexec "github.com/ccfos/huatuo/internal/profiler/exec"
)

const (
	asprofFilePollInterval = 100 * time.Millisecond
	windowProtocol         = "window-completion-v1"
)

type collapsedFile struct {
	path     string
	sequence uint64
}

type collapsedFileCollector struct {
	pids           []int
	pidsToFilePath map[int]string
	enqueueSample  func(profiler.SampleOutput) error
	nextSequence   map[int]uint64
}

func newCollapsedFileCollector(pids []int, paths map[int]string, enqueue func(profiler.SampleOutput) error) *collapsedFileCollector {
	return &collapsedFileCollector{
		pids: append([]int(nil), pids...), pidsToFilePath: paths,
		enqueueSample: enqueue, nextSequence: make(map[int]uint64),
	}
}

func (c *collapsedFileCollector) run(ctx context.Context) error {
	ticker := time.NewTicker(asprofFilePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := c.scanOutputFiles(); err != nil {
				return err
			}
			complete, err := c.complete()
			if err != nil {
				return err
			}
			if complete {
				return nil
			}
		}
	}
}

// The agent publishes immutable windows by rename. File size stability cannot
// distinguish a finished window from a paused writer.
func (c *collapsedFileCollector) scanOutputFiles() error {
	var errs []error
	for _, pid := range c.pids {
		files, err := c.listSortedFiles(pid)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, file := range files {
			if file.sequence < c.nextSequence[pid] {
				continue
			}
			if file.sequence != c.nextSequence[pid] {
				break
			}
			data, err := os.ReadFile(file.path)
			if err == nil {
				err = c.enqueueSample(profiler.SampleOutput{PID: pid, Output: string(data)})
			}
			if err != nil {
				errs = append(errs, fmt.Errorf("deliver Java window %q (file retained): %w", file.path, err))
				break
			}
			c.nextSequence[pid]++
			// Queue acceptance is not durable export. Retain the session files
			// for recovery if the controller is killed after this point.
		}
	}
	return errors.Join(errs...)
}

func (c *collapsedFileCollector) listSortedFiles(pid int) ([]collapsedFile, error) {
	paths, err := filepath.Glob(c.pidsToFilePath[pid])
	if err != nil {
		return nil, fmt.Errorf("glob output for pid %d: %w", pid, err)
	}
	files := make([]collapsedFile, 0, len(paths))
	for _, path := range paths {
		seq, err := collapsedFileSequence(path)
		if err != nil {
			return nil, err
		}
		files = append(files, collapsedFile{path: path, sequence: seq})
	}
	slices.SortFunc(files, func(a, b collapsedFile) int { return cmp.Compare(a.sequence, b.sequence) })
	return files, nil
}

func (c *collapsedFileCollector) complete() (bool, error) {
	all := true
	for _, pid := range c.pids {
		path := strings.TrimSuffix(c.pidsToFilePath[pid], "-*.collapsed") + ".complete"
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			all = false
			continue
		}
		if err != nil {
			return false, fmt.Errorf("read Java completion %q: %w", path, err)
		}
		fields := strings.Split(string(data), "\n")
		if len(fields) != 3 || fields[0] != windowProtocol || fields[2] != "" {
			return false, fmt.Errorf("invalid Java completion marker %q", path)
		}
		last, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || last == ^uint64(0) {
			return false, fmt.Errorf("invalid Java final sequence in %q", path)
		}
		// The marker may have appeared after the preceding directory scan.
		if c.nextSequence[pid] < last+1 {
			if err := c.scanOutputFiles(); err != nil {
				return false, err
			}
		}
		if c.nextSequence[pid] != last+1 {
			return false, fmt.Errorf("incomplete Java session for PID %d: delivered %d windows, expected %d; files retained", pid, c.nextSequence[pid], last+1)
		}
	}
	return all, nil
}

func collapsedFileSequence(path string) (uint64, error) {
	base := filepath.Base(path)
	stem, ok := strings.CutSuffix(base, ".collapsed")
	if !ok {
		return 0, fmt.Errorf("collapsed output %q has no sequence", path)
	}
	separator := strings.LastIndexByte(stem, '-')
	if separator < 0 || separator == len(stem)-1 {
		return 0, fmt.Errorf("collapsed output %q has no sequence", path)
	}

	sequenceText := stem[separator+1:]
	sequence, err := strconv.ParseUint(sequenceText, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid sequence %q in collapsed output %q: %w", sequenceText, path, err)
	}
	return sequence, nil
}

func finishAsprofSampling(ctx context.Context, opt *AsprofSamplingOption, collector *collapsedFileCollector) error {
	stopErr := stopAsprofSampling(ctx, opt)
	scanErr := collector.scanOutputFiles()
	complete, completeErr := collector.complete()
	if !complete && completeErr == nil {
		completeErr = fmt.Errorf("Java completion marker missing; session files retained")
	}
	return errors.Join(stopErr, scanErr, completeErr)
}

func stopAsprofSampling(ctx context.Context, opt *AsprofSamplingOption) error {
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), asprofCommandTimeout)
	defer cancel()
	results := profilerexec.RunAsyncProfiler(finalCtx, opt.activePIDList(), asprofPath(opt.ToolPath), func(pid int) []string {
		return finishArgs(pid, opt.SessionID, opt.OutFilePrefix)
	})
	opt.markStopped(results)
	return errors.Join(finalCtx.Err(), profilerexec.Verify(results))
}
