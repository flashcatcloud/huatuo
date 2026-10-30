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

package doris

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/ccfos/huatuo/internal/storage/driver"
	"github.com/ccfos/huatuo/internal/timeutil"
)

// SQLite supplies real database/sql row and cancellation behavior; Doris DDL
// and Stream Load are covered separately by the SQL and HTTP tests.
func queryFixture(t *testing.T) *Storage {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec("CREATE TABLE records (id TEXT PRIMARY KEY, data TEXT, fields TEXT, f_hostname TEXT)"); err != nil {
		t.Fatal(err)
	}
	return &Storage{db: db, database: "main", table: "records", loc: time.UTC, batchMaxRows: 16, kinds: map[string]driver.Kind{"f_hostname": driver.KindString}}
}

func TestQueryBatchOwnershipAndConsumerError(t *testing.T) {
	s := queryFixture(t)
	for i := 0; i < 5; i++ {
		if _, err := s.db.Exec("INSERT INTO records VALUES (?, ?, '{}', '')", fmt.Sprint(i), fmt.Sprint(i)); err != nil {
			t.Fatal(err)
		}
	}
	var batches [][]driver.Record
	err := s.Query(context.Background(), driver.Query{Limit: 4, Offset: 1, BatchSize: 2}, func(records []driver.Record) error { batches = append(batches, records); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(batches[0]) != 2 || batches[0][0].ID != "1" || batches[1][0].ID != "3" {
		t.Fatalf("batches: %+v", batches)
	}
	sentinel := errors.New("consumer stopped")
	calls := 0
	err = s.Query(context.Background(), driver.Query{Limit: 5, BatchSize: 1}, func([]driver.Record) error { calls++; return sentinel })
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestSaveSyncWaitsForVisiblePayload(t *testing.T) {
	s := queryFixture(t)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("group_commit"); got != "" {
			t.Errorf("group_commit=%q", got)
		}
		var rows []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&rows); err != nil {
			t.Error(err)
			http.Error(w, "decode", http.StatusBadRequest)
			return
		}
		for _, row := range rows {
			if _, err := s.db.Exec("INSERT OR REPLACE INTO records VALUES (?, ?, '{}', '')", row["id"], row["data"]); err != nil {
				t.Error(err)
			}
		}
		// Even Publish Timeout must be confirmed through the query connection.
		_, _ = w.Write([]byte(`{"Status":"Publish Timeout"}`))
	}))
	defer server.Close()
	s.loader = loaderFor(server, 0, GroupCommitAsync)
	rec := driver.Record{ID: "window", Data: []byte("new payload")}
	if err := s.Save(context.Background(), rec, driver.SaveOptions{WaitForVisibility: true}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(s.batch) != 0 || s.loader.groupCommit != GroupCommitAsync {
		t.Fatalf("calls=%d batch=%d mode=%s", calls, len(s.batch), s.loader.groupCommit)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.waitVisible(ctx, driver.Record{ID: "window", Data: []byte("not visible")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestUnsupportedWritesDoNotEnqueue(t *testing.T) {
	s := queryFixture(t)
	for _, options := range []driver.SaveOptions{{Mode: driver.SaveModeCreateOnly}, {Mode: driver.SaveModeConditional}, {Conditions: []driver.Filter{{Field: "hostname", Op: driver.OpEq, Value: "a"}}}} {
		if err := s.Save(context.Background(), driver.Record{ID: "id"}, options); !errors.Is(err, driver.ErrUnsupported) {
			t.Fatalf("got %v", err)
		}
	}
	if len(s.batch) != 0 {
		t.Fatal("unsupported write was buffered")
	}
	if _, err := s.DeleteByQuery(context.Background(), driver.DeleteQuery{}); !errors.Is(err, driver.ErrUnsupported) {
		t.Fatalf("got %v", err)
	}
}

func TestBuildRowTimestampAndNull(t *testing.T) {
	s := queryFixture(t)
	s.kinds["f_uploaded_timestamp"] = driver.KindTime
	stamp := timeutil.Timestamp{Time: time.Date(2026, 10, 13, 1, 2, 3, 0, time.UTC)}
	row, _, err := s.buildRow(driver.Record{ID: "id", Fields: map[string]any{"uploaded_timestamp": stamp, "hostname": nil, "undeclared": "retained in fields"}})
	if err != nil {
		t.Fatal(err)
	}
	if row["f_uploaded_timestamp"] != "2026-10-13 01:02:03.000000" || row["f_hostname"] != nil {
		t.Fatalf("row=%v", row)
	}
	if _, ok := row["f_undeclared"]; ok {
		t.Fatal("undeclared column emitted")
	}
}

func TestFilterPresenceAndAggregatePagination(t *testing.T) {
	s := queryFixture(t)
	for i, value := range []any{nil, "", "node"} {
		if _, err := s.db.Exec("INSERT INTO records VALUES (?, '', '{}', ?)", fmt.Sprint(i), value); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		op    driver.Op
		value any
		want  int64
	}{{driver.OpExists, nil, 2}, {driver.OpNotExists, nil, 1}, {driver.OpEq, "", 1}, {driver.OpNe, "node", 1}, {driver.OpIn, []string{"", "node"}, 2}} {
		got, err := s.Count(context.Background(), driver.Query{Filters: []driver.Filter{{Field: "hostname", Op: test.op, Value: test.value}}, Limit: -1, Offset: -1})
		if err != nil || got != test.want {
			t.Fatalf("%s: count=%d err=%v", test.op, got, err)
		}
	}
	got, err := s.Values(context.Background(), "hostname", driver.Query{Limit: -1, Offset: -1}, 10)
	if err != nil || !reflect.DeepEqual(got, []string{"", "node"}) {
		t.Fatalf("values=%v err=%v", got, err)
	}
	for _, filter := range []driver.Filter{{Field: "hostname", Op: driver.OpEq}, {Field: "hostname", Op: driver.OpExists, Value: "x"}, {Field: "hostname", Op: driver.OpIn, Value: []string{}}} {
		if _, _, err := buildWhereSQL([]driver.Filter{filter}, time.UTC); err == nil {
			t.Fatalf("accepted %+v", filter)
		}
	}
}
