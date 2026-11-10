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
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"

	v1 "huatuo-bamai/apis/v1"
	"huatuo-bamai/cmd/huatuo-bamai/config"
	"huatuo-bamai/internal/server"
	"huatuo-bamai/internal/server/response"
	"huatuo-bamai/pkg/tracing"

	"github.com/go-playground/validator/v10"
)

type TaskHandler struct {
	Handlers []server.Route
}

func NewTaskHandler() *TaskHandler {
	h := &TaskHandler{}
	h.Handlers = []server.Route{
		{Method: http.MethodPost, Path: "", Handler: h.create},
		{Method: http.MethodGet, Path: "", Handler: h.list},
		{Method: http.MethodGet, Path: "/:id", Handler: h.get},
		{Method: http.MethodDelete, Path: "/:id", Handler: h.stop},
	}
	return h
}

type NewTaskReq struct {
	RequestID  string   `json:"request_id" binding:"omitempty,max=128"`
	TracerName string   `json:"tracer_name" binding:"required"`
	Timeout    int      `json:"timeout" binding:"required,number,lt=3600"`
	DataType   string   `json:"data_type" binding:"required"`
	TracerArgs []string `json:"trace_args" binding:"omitempty"`
}

// Set at link time only for the Flashcat host profiling release.
var hostCPURelease string

var hostJobID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)

func validateHostCPU(req NewTaskReq, executable string) error {
	a := req.TracerArgs
	invalid := errors.New("expected a host CPU profiler request with package-owned tools and output")
	if req.TracerName != "profiler" || req.DataType != "db-json" || !hostJobID.MatchString(req.RequestID) ||
		req.Timeout < 40 || req.Timeout >= 3600 || len(a) < 6 || a[0] != "-t" || a[1] != "cpu" || a[2] != "-l" || a[4] != "--pid" {
		return invalid
	}
	pid, err := strconv.ParseInt(a[5], 10, 32)
	if err != nil || pid <= 0 {
		return invalid
	}
	lang := a[3]
	a = a[6:]
	switch lang {
	case "java", "python":
		if len(a) < 2 || a[0] != "--tool-path" || a[1] != filepath.Dir(filepath.Dir(executable)) {
			return invalid
		}
		a = a[2:]
	case "go", "c++":
		if len(a) < 1 || a[0] != "--thread-group" {
			return invalid
		}
		a = a[1:]
	default:
		return invalid
	}
	duration := req.Timeout - 20
	interval := 10
	if lang == "python" {
		interval = duration
	}
	if !slices.Equal(a, []string{
		"--tracer-id", req.RequestID, "--duration", strconv.Itoa(duration),
		"--aggr-interval", strconv.Itoa(interval), "--max-concurrent-procs", "10",
		"--output-format", "remote", "--output-storage", "/var/run/huatuo-toolstream.sock",
	}) {
		return invalid
	}
	return nil
}

func handleBindError(ctx *server.Context, err error) {
	var validationError *validator.ValidationErrors
	if errors.As(err, &validationError) {
		response.ErrorWithCode(
			ctx,
			http.StatusBadRequest,
			v1.ErrorCodeInvalidRequest,
			(*validationError)[0].Namespace(),
		)
		return
	}
	response.ErrorWithCode(ctx, http.StatusBadRequest, v1.ErrorCodeInvalidRequest, err.Error())
}

func (h *TaskHandler) create(ctx *server.Context) error {
	var req NewTaskReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		handleBindError(ctx, err)
		return nil
	}

	if hostCPURelease == "true" {
		exe, err := os.Executable()
		if err != nil {
			return response.ErrInternal.WithMessage("cannot locate packaged profiler tools")
		}
		if err := validateHostCPU(req, exe); err != nil {
			return response.ErrInvalidRequest.WithMessage(err.Error())
		}
	}
	storageDefault := tracing.TaskStorageDB
	if req.DataType == "json" {
		storageDefault = tracing.TaskStorageStdout
	}

	id := req.RequestID
	if id == "" {
		var err error
		id, err = tracing.AllocTaskID()
		if err != nil {
			return response.ErrInternal.WithMessage("failed to allocate task id")
		}
	}
	id, err := tracing.NewTaskWithIDLimit(
		id,
		req.TracerName,
		time.Duration(req.Timeout)*time.Second,
		storageDefault,
		req.TracerArgs,
		config.Get().Tasks.MaxConcurrent,
	)
	if err != nil {
		if errors.Is(err, tracing.ErrTaskLimitExceeded) {
			return response.ErrInvalidRequest.WithMessage(err.Error())
		}
		return response.ErrInvalidRequest.WithMessage(err.Error())
	}
	response.Success(ctx, map[string]any{"task_id": id})
	return nil
}

func (h *TaskHandler) list(ctx *server.Context) error {
	response.Success(ctx, map[string]any{"tasks": tracing.ListTasks()})
	return nil
}

func (h *TaskHandler) get(ctx *server.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return response.ErrInvalidRequest.WithMessage("missing task id")
	}

	result := tracing.Result(id)
	responseData := map[string]any{"status": result.TaskStatus}
	switch result.TaskStatus {
	case tracing.StatusCompleted:
		responseData["data"] = string(result.TaskData)
	case tracing.StatusNotExist, tracing.StatusFailed:
		responseData["error"] = result.TaskErr.Error()
	}

	response.Success(ctx, responseData)
	return nil
}

func (h *TaskHandler) stop(ctx *server.Context) error {
	taskID := ctx.Param("id")
	if taskID == "" {
		return response.ErrInvalidRequest.WithMessage("missing task id")
	}

	if err := tracing.StopTask(taskID); err != nil {
		if errors.Is(err, tracing.ErrTaskNotFound) {
			return response.ErrNotFound.WithMessage("task not found")
		}
		return response.ErrInternal.WithMessage(err.Error())
	}

	ctx.Status(http.StatusNoContent)
	return nil
}
