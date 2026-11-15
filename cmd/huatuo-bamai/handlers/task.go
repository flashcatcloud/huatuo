// Copyright 2025, 2026 The HuaTuo Authors
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

package handlers

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/ccfos/huatuo/internal/nodeagent/operation"
	nodeprofiling "github.com/ccfos/huatuo/internal/nodeagent/profiling"
	"github.com/ccfos/huatuo/internal/server"
	"github.com/ccfos/huatuo/internal/server/response"
	"github.com/ccfos/huatuo/pkg/observation"
	"github.com/ccfos/huatuo/pkg/profiling"
)

type NewTaskReq struct {
	RequestID  string   `json:"request_id" binding:"omitempty,max=128"`
	TracerName string   `json:"tracer_name" binding:"required"`
	Timeout    int      `json:"timeout" binding:"required,number,lt=3600"`
	DataType   string   `json:"data_type" binding:"required"`
	TracerArgs []string `json:"trace_args" binding:"omitempty"`
}

var hostJobID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

func parseHostTask(req *NewTaskReq, executable string) (*nodeprofiling.StartRequest, error) {
	a := req.TracerArgs
	invalid := errors.New("invalid host profiling arguments: expected a supported mode with package-owned tools and output")
	if req.TracerName != "profiler" || req.DataType != "db-json" || !hostJobID.MatchString(req.RequestID) ||
		req.Timeout < 40 || req.Timeout >= 3600 || len(a) < 6 || a[0] != "-t" {
		return nil, invalid
	}
	spec := profiling.Spec{Type: profiling.Type(a[1]), Mode: profiling.ModeOnCPU}
	var phase profiling.OffCPUPhase
	a = a[2:]
	switch spec.Type {
	case profiling.TypeCPU:
		if len(a) >= 4 && a[0] == "--cpu-mode" && a[1] == "offcpu" && a[2] == "--offcpu-phase" {
			var err error
			phase, err = profiling.ParseOffCPUPhase(a[3])
			if err != nil {
				return nil, err
			}
			spec.Mode = profiling.ModeOffCPU
			a = a[4:]
		}
	case profiling.TypeMemory:
		if len(a) < 2 || a[0] != "--memory-mode" {
			return nil, invalid
		}
		spec.Mode = profiling.Mode(a[1])
		a = a[2:]
	default:
		return nil, invalid
	}
	if len(a) < 4 || a[0] != "-l" || a[2] != "--pid" {
		return nil, invalid
	}
	spec.Language = profiling.Language(a[1])
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	pid, err := strconv.ParseInt(a[3], 10, 32)
	if err != nil || pid <= 0 {
		return nil, invalid
	}
	lang := a[1]
	a = a[4:]
	switch lang {
	case "java", "python":
		if len(a) > 0 && a[0] == "--tool-path" {
			if len(a) < 2 || a[1] != filepath.Dir(filepath.Dir(executable)) {
				return nil, invalid
			}
			a = a[2:]
		}
	case "c", "go", "c++":
		if len(a) < 1 || a[0] != "--thread-group" {
			return nil, invalid
		}
		a = a[1:]
	default:
		return nil, invalid
	}
	duration := req.Timeout - 20
	interval := 10
	if lang == "python" && len(a) > 5 && a[5] == strconv.Itoa(duration) {
		interval = duration
	}
	if !slices.Equal(a, []string{
		"--tracer-id", req.RequestID, "--duration", strconv.Itoa(duration),
		"--aggr-interval", strconv.Itoa(interval), "--max-concurrent-procs", "10",
		"--output-format", "remote", "--output-storage", "/var/run/huatuo-toolstream.sock",
	}) {
		return nil, invalid
	}
	return &nodeprofiling.StartRequest{RequestID: req.RequestID, HostPID: int(pid), Duration: time.Duration(duration) * time.Second, Scope: observation.ScopeHost, Spec: spec, OffCPUPhase: phase}, nil
}

func hostTaskRoutes(opts *ServerOptions) []server.Route {
	routes := []server.Route{
		{Method: http.MethodPost, Path: "", Handler: func(ctx *server.Context) error {
			var req NewTaskReq
			if err := ctx.ShouldBindJSON(&req); err != nil {
				return response.ErrInvalidRequest.WithMessage(err.Error())
			}
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			request, err := parseHostTask(&req, exe)
			if err != nil {
				return response.ErrInvalidRequest.WithMessage(err.Error())
			}
			op, _, err := opts.ProfilingService.Start(ctx.Request().Context(), request)
			if err != nil {
				return nodeAPIError(err)
			}
			response.Success(ctx, map[string]string{"task_id": op.RequestID})
			return nil
		}},
		{Method: http.MethodGet, Path: "/:id", Handler: func(ctx *server.Context) error {
			op, err := opts.OperationManager.GetByID(ctx.Param("id"))
			if err != nil {
				return nodeAPIError(err)
			}
			result := map[string]string{"status": "running"}
			if op.Terminal != nil {
				result["status"] = string(op.Terminal.Outcome)
				if op.Terminal.Outcome != operation.OutcomeCompleted {
					result["status"] = "failed"
					result["error"] = op.Terminal.Message
				}
			}
			response.Success(ctx, result)
			return nil
		}},
		{Method: http.MethodDelete, Path: "/:id", Handler: func(ctx *server.Context) error {
			_, _, err := opts.OperationManager.StopByID(ctx.Param("id"))
			if err != nil {
				return nodeAPIError(err)
			}
			ctx.Status(http.StatusNoContent)
			return nil
		}},
	}
	for i := range routes {
		handler := routes[i].Handler
		routes[i].Handler = func(ctx *server.Context) error {
			host, _, err := net.SplitHostPort(ctx.Request().RemoteAddr)
			if err != nil || !net.ParseIP(host).IsLoopback() {
				ctx.Status(http.StatusForbidden)
				return nil
			}
			return handler(ctx)
		}
	}
	return routes
}
