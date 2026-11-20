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

package pod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestKubeletTokenClientReReadsRotatedToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "token")
	old := serviceAccountTokenPath
	serviceAccountTokenPath = tokenPath
	t.Cleanup(func() { serviceAccountTokenPath = old })

	var seen []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	if _, err := kubeletAuthorizedClient(&ManagerCtx{}); err == nil {
		t.Fatal("built a token client without a token file")
	}
	if err := os.WriteFile(tokenPath, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := kubeletAuthorizedClient(&ManagerCtx{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kubeletFetchPodList(context.Background(), client, srv.URL+"/pods"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := kubeletFetchPodList(context.Background(), client, srv.URL+"/pods"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != "Bearer first" || seen[1] != "Bearer second" {
		t.Fatalf("authorization headers = %q", seen)
	}
}

func TestInitManagerRequiresCertOrToken(t *testing.T) {
	old := serviceAccountTokenPath
	serviceAccountTokenPath = filepath.Join(t.TempDir(), "missing")
	t.Cleanup(func() { serviceAccountTokenPath = old })

	if err := InitManager(&ManagerCtx{PodAuthorizedPort: 10250}); err == nil {
		ReleaseManager()
		t.Fatal("started without a client certificate or token")
	}
}
