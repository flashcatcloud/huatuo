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

package main

import (
	"testing"

	"github.com/ccfos/huatuo/cmd/huatuo-bamai/config"
	internalconfig "github.com/ccfos/huatuo/internal/config"
)

func TestProfileStorageSelection(t *testing.T) {
	cfg := &config.Config{}
	if got := profileStorageConfig(cfg); got != nil {
		t.Fatalf("disabled selection=%+v", got)
	}
	cfg.Storage.Elasticsearch = internalconfig.ElasticsearchConfig{Address: "http://localhost:9200", Username: "test", Password: "test", Index: "existing"}
	if got := profileStorageConfig(cfg); got == nil || got.Driver != "elasticsearch" || got.ESIndex != "existing" {
		t.Fatalf("ES selection=%+v", got)
	}
	cfg.Storage.Doris = internalconfig.DorisConfig{MySQLAddr: "localhost:9030", HTTPAddr: "localhost:8030", Database: "profiles", PartitionField: "uploaded_timestamp"}
	if got := profileStorageConfig(cfg); got == nil || got.Driver != "doris" || got.DorisPartitionField != "uploaded_timestamp" {
		t.Fatalf("Doris selection=%+v", got)
	}
}
