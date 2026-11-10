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

package store

import (
	"encoding/json"
	"github.com/ccfos/huatuo/internal/timeutil"
	"strings"
	"time"

	"github.com/ccfos/huatuo/internal/storage/driver"
	"github.com/ccfos/huatuo/pkg/types"

	"github.com/rs/xid"
)

const (
	// Collection is the storage collection for profiling aggregation windows.
	Collection = "profiling_metadata"

	fieldProfileType = "profile_data.profile_type"
)

type mapper struct{ hostDoris bool }

const hostTimeLayout = "2006-01-02 15:04:05.000 -0700"

type hostFlameData struct {
	FlameData *ProfileData `json:"flamedata"`
}
type hostDocument struct {
	types.Document
	Uploaded   timeutil.Timestamp `json:"uploaded_time"`
	Time       string             `json:"time"`
	TracerTime string             `json:"tracer_time"`
	TracerData hostFlameData      `json:"tracer_data"`
}

func (m mapper) field(key string) string {
	if !m.hostDoris {
		return key
	}
	base := strings.TrimSuffix(key, ".keyword")
	var name string
	switch base {
	case types.DocumentFieldUploadedTimestamp:
		name = "uploaded_time"
	case types.DocumentFieldStartedTimestamp:
		name = "time"
	case fieldProfileType:
		name = "tracer_data.flamedata.profile_type"
	default:
		return key
	}
	return name + strings.TrimPrefix(key, base)
}

func (mapper) ID(*Document) string {
	return xid.New().String()
}

func (m mapper) Encode(document *Document) ([]byte, error) {
	if m.hostDoris {
		if err := document.validate(); err != nil {
			return nil, err
		}
		stamp := document.StartedTimestamp.Format(hostTimeLayout)
		return json.Marshal(hostDocument{Document: document.Document, Uploaded: document.UploadedTimestamp, Time: stamp, TracerTime: stamp, TracerData: hostFlameData{document.ProfileData}})
	}
	return json.Marshal(document)
}

func (m mapper) Decode(record driver.Record) (*Document, error) {
	var document Document
	if m.hostDoris {
		var host hostDocument
		if err := json.Unmarshal(record.Data, &host); err != nil {
			return nil, err
		}
		started, err := time.Parse(hostTimeLayout, host.TracerTime)
		if err != nil {
			return nil, err
		}
		document.Document = host.Document
		document.UploadedTimestamp = host.Uploaded
		document.StartedTimestamp = &timeutil.Timestamp{Time: started}
		document.ProfileData = host.TracerData.FlameData
	} else if err := json.Unmarshal(record.Data, &document); err != nil {
		return nil, err
	}
	if err := document.validate(); err != nil {
		return nil, err
	}
	return &document, nil
}

func (m mapper) Fields(document *Document) (map[string]any, error) {
	if err := document.validate(); err != nil {
		return nil, err
	}
	fields := map[string]any{
		types.DocumentFieldHostname:               document.Hostname,
		types.DocumentFieldRegion:                 document.Region,
		types.DocumentFieldUploadedTimestamp:      document.UploadedTimestamp,
		types.DocumentFieldStartedTimestamp:       *document.StartedTimestamp,
		types.DocumentFieldContainerID:            document.ContainerID,
		types.DocumentFieldContainerHostname:      document.ContainerHostname,
		types.DocumentFieldContainerHostNamespace: document.ContainerHostNamespace,
		types.DocumentFieldContainerType:          document.ContainerType,
		types.DocumentFieldContainerQoS:           document.ContainerQoS,
		types.DocumentFieldTracerName:             document.TracerName,
		types.DocumentFieldTracerID:               document.TracerID,
		types.DocumentFieldTracerType:             document.TracerRunType,
		fieldProfileType:                          document.ProfileData.ProfileType,
	}
	if document.ContainerHostname == "" {
		delete(fields, types.DocumentFieldContainerHostname)
	}
	if m.hostDoris {
		for _, key := range []string{types.DocumentFieldUploadedTimestamp, types.DocumentFieldStartedTimestamp, fieldProfileType} {
			fields[m.field(key)] = fields[key]
			delete(fields, key)
		}
		fields["uploaded_time"] = document.UploadedTimestamp.Time
		fields["time"] = document.StartedTimestamp.Time
		fields["tracer_time"] = document.StartedTimestamp.Time
	}
	return fields, nil
}

func (m mapper) Indexes() []driver.Index {
	indexes := []driver.Index{
		{Field: types.DocumentFieldTracerID},
		{Field: types.DocumentFieldHostname},
		{Field: types.DocumentFieldRegion},
		{Field: types.DocumentFieldUploadedTimestamp, Kind: driver.KindTime},
		{Field: types.DocumentFieldStartedTimestamp, Kind: driver.KindTime},
		{Field: types.DocumentFieldContainerID},
		{Field: types.DocumentFieldContainerHostname},
		{Field: types.DocumentFieldContainerHostNamespace},
		{Field: types.DocumentFieldContainerType},
		{Field: types.DocumentFieldContainerQoS},
		{Field: types.DocumentFieldTracerName},
		{Field: types.DocumentFieldTracerType},
		{Field: fieldProfileType},
	}
	if m.hostDoris {
		for i := range indexes {
			indexes[i].Field = m.field(indexes[i].Field)
		}
		indexes = append(indexes, driver.Index{Field: "tracer_time", Kind: driver.KindTime})
	}
	return indexes
}
