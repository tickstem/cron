package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tickstem/cron"
)

//go:embed dashboard.html
var dashboardHTML string

type server struct {
	store     *store
	scheduler *scheduler
	log       *slog.Logger
}

func newServer(store *store, sched *scheduler, log *slog.Logger) *server {
	return &server{store: store, scheduler: sched, log: log}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	// API — mirrors the Tickstem platform API contract
	mux.HandleFunc("POST /v1/jobs", s.registerJob)
	mux.HandleFunc("GET /v1/jobs", s.listJobs)
	mux.HandleFunc("GET /v1/jobs/{id}", s.getJob)
	mux.HandleFunc("PUT /v1/jobs/{id}", s.updateJob)
	mux.HandleFunc("PATCH /v1/jobs/{id}", s.updateJobStatus)
	mux.HandleFunc("DELETE /v1/jobs/{id}", s.deleteJob)
	mux.HandleFunc("GET /v1/executions", s.listExecutions)

	// tsk-local extension — not on the real platform
	mux.HandleFunc("POST /v1/jobs/{id}/trigger", s.triggerJob)

	mux.HandleFunc("GET /", s.dashboard)

	return mux
}

func (s *server) registerJob(w http.ResponseWriter, r *http.Request) {
	var params cron.RegisterParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if params.Name == "" || params.Schedule == "" || params.Endpoint == "" {
		writeError(w, http.StatusUnprocessableEntity, "name, schedule, and endpoint are required")
		return
	}
	if params.Method == "" {
		params.Method = http.MethodPost
	}
	if params.TimeoutSecs == 0 {
		params.TimeoutSecs = 30
	}

	now := time.Now().UTC()
	job := cron.Job{
		ID:          "job_" + randomID(),
		Name:        params.Name,
		Description: params.Description,
		Schedule:    params.Schedule,
		Endpoint:    params.Endpoint,
		Method:      params.Method,
		TimeoutSecs: params.TimeoutSecs,
		Status:      "active",
		CreatedAt:   now,
	}

	entryID, nextRun, err := s.scheduler.addJob(job)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("invalid cron expression: %s", err))
		return
	}
	job.NextRunAt = &nextRun
	s.store.addJob(job, entryID)

	s.log.Info("job registered", "id", job.ID, "name", job.Name, "schedule", job.Schedule, "next_run", nextRun.Format(time.RFC3339))
	writeJSON(w, http.StatusCreated, job)
}

func (s *server) listJobs(w http.ResponseWriter, _ *http.Request) {
	jobs := s.store.listJobs()
	if jobs == nil {
		jobs = []cron.Job{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs, "limit": 100, "offset": 0})
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.store.getJob(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *server) updateJob(w http.ResponseWriter, r *http.Request) {
	var params cron.RegisterParams
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	jobID := r.PathValue("id")
	job, ok := s.store.updateJobParams(jobID, params)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	s.log.Info("job updated", "id", jobID)
	writeJSON(w, http.StatusOK, job)
}

func (s *server) updateJobStatus(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	jobID := r.PathValue("id")
	job, ok := s.store.updateJobStatus(jobID, body.Status)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	s.log.Info("job status updated", "id", jobID, "status", body.Status)
	writeJSON(w, http.StatusOK, job)
}

func (s *server) deleteJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	entryID, ok := s.store.deleteJob(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	s.scheduler.removeJob(entryID)

	s.log.Info("job deleted", "id", jobID)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listExecutions(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "job_id query parameter is required")
		return
	}
	if _, ok := s.store.getJob(jobID); !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	executions := s.store.listExecutions(jobID)
	if executions == nil {
		executions = []cron.Execution{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": executions, "limit": 100, "offset": 0})
}

func (s *server) triggerJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	job, ok := s.store.getJob(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	s.log.Info("manual trigger", "id", jobID, "name", job.Name)
	go s.runJob(job)

	writeJSON(w, http.StatusAccepted, map[string]string{"message": "triggered"})
}

func (s *server) dashboard(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

// runJob is called by both the scheduler and manual triggers.
func (s *server) runJob(job cron.Job) {
	now := time.Now().UTC()
	execID := "exec_" + randomID()

	exec := cron.Execution{
		ID:          execID,
		JobID:       job.ID,
		Status:      cron.ExecutionStatus("running"),
		ScheduledAt: now,
		StartedAt:   &now,
	}
	s.store.addExecution(exec)
	s.log.Info("execution started", "job", job.Name, "exec_id", execID)

	result := executeJob(job)

	finished := time.Now().UTC()
	exec.FinishedAt = &finished
	exec.DurationMs = &result.durationMs

	if result.err != "" {
		exec.Status = cron.ExecutionStatusFailed
		exec.Error = result.err
		s.log.Error("execution failed", "job", job.Name, "exec_id", execID, "error", result.err, "duration_ms", result.durationMs)
	} else {
		exec.Status = cron.ExecutionStatusSuccess
		exec.StatusCode = &result.statusCode
		s.log.Info("execution succeeded", "job", job.Name, "exec_id", execID, "status_code", result.statusCode, "duration_ms", result.durationMs)
	}

	s.store.updateExecution(exec)

	if nextRun, ok := s.scheduler.nextRun(job.ID); ok {
		s.store.updateJobNextRun(job.ID, nextRun)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
