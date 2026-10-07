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
	if n := len(got.Profile.Sample); n == 0 {
		t.Fatal("empty trailer discarded the three windows that had stacks")
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
	if n := len(got.Profile.Sample); n == 0 {
		t.Fatal("raw collapsed text produced no samples")
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
	if n := len(got.Profile.Sample); n == 0 {
		t.Fatal("empty trailer discarded windows that had stacks")
	}
}
