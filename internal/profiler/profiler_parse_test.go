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

package profiler

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// asprof --loop dumps a trailing empty file when stop races the last window.
// That empty Output used to send the whole JSON array down the raw-text path and
// drop every window that already had stacks.
func TestParseCollapsedDataKeepsWindowsWhenTrailerEmpty(t *testing.T) {
	pid := os.Getpid()
	data, err := json.Marshal([]SampleOutput{
		{PID: pid, Output: "a;b 1\nc;d 2\ne;f 3\n"},
		{PID: pid, Output: "g;h 1\ni;j 1\nk;l 1\nm;n 1\no;p 1\nq;r 1\ns;t 1\n"},
		{PID: pid, Output: "u;v 1\nw;x 1\ny;z 1\naa;bb 1\ncc;dd 1\nee;ff 1\n"},
		{PID: 5, Output: ""},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseCollapsedData(&ParseInput{
		StartTime:    time.Unix(0, 0),
		ProfileType:  ProfileTypeMemSample,
		ProfilerName: "java-mem",
		Data:         data,
		PID:          pid,
	})
	if err != nil {
		t.Fatalf("ParseCollapsedData: %v", err)
	}
	if gotCount := sampleCount(got); gotCount != 19 {
		t.Fatalf("sample total=%d, want 19", gotCount)
	}
}

func TestParseCollapsedDataRawCollapsedText(t *testing.T) {
	pid := os.Getpid()
	got, err := ParseCollapsedData(&ParseInput{
		StartTime:    time.Unix(0, 0),
		ProfileType:  ProfileTypeMemSample,
		ProfilerName: "java-mem",
		Data:         []byte("foo;bar 4\n"),
		PID:          pid,
	})
	if err != nil {
		t.Fatalf("ParseCollapsedData: %v", err)
	}
	if gotCount := sampleCount(got); gotCount != 4 {
		t.Fatalf("sample total=%d, want 4", gotCount)
	}
}

func TestParseRawDataKeepsWindowsWhenTrailerEmpty(t *testing.T) {
	pid := os.Getpid()
	data, err := json.Marshal([]SampleOutput{
		{PID: pid, Output: "a;b 1\nc;d 2\n"},
		{PID: 5, Output: ""},
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ParseRawData(&ParseInput{
		StartTime:    time.Unix(0, 0),
		ProfileType:  ProfileTypeCpuSample,
		ProfilerName: "python-cpu",
		Data:         data,
		PID:          pid,
	})
	if err != nil {
		t.Fatalf("ParseRawData: %v", err)
	}
	if gotCount := sampleCount(got); gotCount != 3 {
		t.Fatalf("sample total=%d, want 3", gotCount)
	}
}

func sampleCount(data *ProfileData) int64 {
	var total int64
	for _, sample := range data.Profile.Sample {
		for _, value := range sample.Value {
			total += value
		}
	}
	return total
}

func TestParseRawDataRemovesPyspyDiagnostics(t *testing.T) {
	data, err := json.Marshal([]SampleOutput{{PID: os.Getpid(), Output: "py-spy> diagnostic;not_a_frame 999\nfoo;bar 7\n"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseRawData(&ParseInput{StartTime: time.Unix(0, 0), ProfileType: ProfileTypeCpuSample, ProfilerName: "python-cpu", Data: data})
	if err != nil {
		t.Fatal(err)
	}
	if count := sampleCount(got); count != 7 {
		t.Fatalf("sample count=%d want 7", count)
	}
}

func TestEmptyJSONWindowsDoNotFallBackToProcessLookup(t *testing.T) {
	for _, parse := range []func(*ParseInput) (*ProfileData, error){ParseCollapsedData, ParseRawData} {
		for _, data := range []string{`[]`, `[{"pid":999999999,"output":""}]`, `[{"pid":999999999,"output":" \n"}]`} {
			got, err := parse(&ParseInput{StartTime: time.Unix(0, 0), ProfileType: ProfileTypeCpuSample, Data: []byte(data)})
			if err != nil {
				t.Fatalf("%s: %v", data, err)
			}
			if count := sampleCount(got); count != 0 {
				t.Fatalf("empty window count=%d", count)
			}
		}
	}
}
