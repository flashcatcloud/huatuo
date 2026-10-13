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
	"context"
	"errors"
	"fmt"

	"github.com/ccfos/huatuo/cmd/huatuo-apiserver/config"
	"github.com/ccfos/huatuo/internal/log"
	"github.com/ccfos/huatuo/internal/profiling/publication"
	profilequery "github.com/ccfos/huatuo/internal/profiling/query"
	"github.com/ccfos/huatuo/internal/storage/driver"
	"github.com/ccfos/huatuo/internal/strutil"
	profilingstore "github.com/ccfos/huatuo/pkg/profiling/store"
)

func setupProfileQueryService(ctx context.Context, d *Daemon) (func(context.Context) error, error) {
	storageConfig := profileStorageConfig(d.opts.Config)
	if storageConfig == nil {
		log.Info("profile storage disabled")
		return nil, nil
	}

	profileStorage, err := profilingstore.NewWithBackendConfig(ctx, storageConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize profile storage: %w", err)
	}
	profileQueryService, err := profilequery.NewProfileQueryService(profileStorage)
	if err != nil {
		_ = profileStorage.Close(ctx)
		return nil, err
	}
	publicationStore, err := publication.NewFromConfig(ctx, storageConfig)
	if err != nil {
		_ = profileStorage.Close(ctx)
		return nil, fmt.Errorf("initialize profiling publication Store: %w", err)
	}
	d.profileStorage = profileStorage
	d.profileQueryService = profileQueryService
	d.publications = publicationStore

	return func(ctx context.Context) error {
		return errors.Join(profileStorage.Close(ctx), publicationStore.Close(ctx))
	}, nil
}

func profileStorageConfig(cfg *config.Config) *driver.Config {
	if cfg.Doris.Enabled() {
		return cfg.Doris.DriverConfig()
	}
	if cfg.Elasticsearch.Enabled() {
		return &driver.Config{
			Driver:      "elasticsearch",
			ESAddresses: strutil.SplitCommaList(cfg.Elasticsearch.Address),
			ESUsername:  cfg.Elasticsearch.Username,
			ESPassword:  cfg.Elasticsearch.Password,
			ESIndex:     cfg.Elasticsearch.Index,
		}
	}
	return nil
}
