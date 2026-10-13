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

package config

import "testing"

func TestDorisConfigurationValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     DorisConfig
		invalid bool
	}{
		{name: "disabled"},
		{name: "missing http", cfg: DorisConfig{MySQLAddr: "localhost:9030"}, invalid: true},
		{name: "missing mysql", cfg: DorisConfig{HTTPAddr: "localhost:8030"}, invalid: true},
		{name: "missing database", cfg: DorisConfig{MySQLAddr: "localhost:9030", HTTPAddr: "localhost:8030"}, invalid: true},
		{name: "invalid group", cfg: DorisConfig{MySQLAddr: "localhost:9030", HTTPAddr: "localhost:8030", Database: "test", GroupCommit: "invalid"}, invalid: true},
		{name: "enabled", cfg: DorisConfig{MySQLAddr: "localhost:9030", HTTPAddr: "localhost:8030", Database: "test", GroupCommit: "async_mode"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.cfg.Validate(); (err != nil) != tc.invalid {
				t.Fatalf("Validate=%v", err)
			}
		})
	}
}
