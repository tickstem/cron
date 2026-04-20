package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/tickstem/cron"
)

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
	mux.HandleFunc("PATCH /v1/jobs/{id}", s.updateJobStatus)
	mux.HandleFunc("DELETE /v1/jobs/{id}", s.deleteJob)
	mux.HandleFunc("GET /v1/jobs/{id}/executions", s.listExecutions)

	// tsk-local extension — not on the real platform
	mux.HandleFunc("POST /v1/jobs/{id}/trigger", s.triggerJob)

	// Dashboard UI
	mux.HandleFunc("GET /", s.dashboard)

	return mux
}

// ── API handlers ──────────────────────────────────────────────────────────────

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

func (s *server) listJobs(w http.ResponseWriter, r *http.Request) {
	jobs := s.store.listJobs()
	if jobs == nil {
		jobs = []cron.Job{}
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.store.getJob(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
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
	jobID := r.PathValue("id")
	if _, ok := s.store.getJob(jobID); !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	executions := s.store.listExecutions(jobID)
	if executions == nil {
		executions = []cron.Execution{}
	}
	writeJSON(w, http.StatusOK, executions)
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

// ── Dashboard ─────────────────────────────────────────────────────────────────

func (s *server) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, dashboardHTML) //nolint:errcheck
}

// ── Job runner (called by scheduler and manual trigger) ───────────────────────

func (s *server) runJob(job cron.Job) {
	now := time.Now().UTC()
	execID := "exec_" + randomID()

	running := cron.ExecutionStatus("running")
	exec := cron.Execution{
		ID:          execID,
		JobID:       job.ID,
		Status:      running,
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

	// Update next run time
	if nextRun, ok := s.scheduler.nextRun(job.ID); ok {
		s.store.updateJobNextRun(job.ID, nextRun)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func randomID() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// dashboardHTML is the built-in web UI — no external assets required.
const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>tsk-local</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body   { font-family: ui-monospace,'Cascadia Code','Fira Code',monospace; background: #09090b; color: #e4e4e7; font-size: 13px; min-height: 100vh; padding: 24px; }
  h1     { font-size: 16px; font-weight: 600; color: #a78bfa; letter-spacing: -0.02em; margin-bottom: 4px; }
  .sub   { color: #71717a; font-size: 12px; margin-bottom: 24px; }
  table  { width: 100%; border-collapse: collapse; margin-bottom: 24px; }
  th     { text-align: left; padding: 6px 12px; color: #71717a; font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; border-bottom: 1px solid #27272a; }
  td     { padding: 8px 12px; border-bottom: 1px solid #1f1f23; vertical-align: top; }
  tr:hover td { background: #18181b; }
  .tag   { display: inline-block; padding: 1px 7px; border-radius: 999px; font-size: 11px; border: 1px solid; }
  .active  { background: rgba(34,197,94,.1);  color: #86efac; border-color: rgba(34,197,94,.25);  }
  .paused  { background: rgba(245,158,11,.1); color: #fcd34d; border-color: rgba(245,158,11,.25); }
  .success { background: rgba(34,197,94,.1);  color: #86efac; border-color: rgba(34,197,94,.25);  }
  .failed  { background: rgba(239,68,68,.1);  color: #fca5a5; border-color: rgba(239,68,68,.25);  }
  .running { background: rgba(56,189,248,.1); color: #7dd3fc; border-color: rgba(56,189,248,.25); }
  button { cursor: pointer; background: transparent; border: 1px solid #3f3f46; color: #a1a1aa; padding: 3px 10px; border-radius: 4px; font-family: inherit; font-size: 11px; transition: all .1s; }
  button:hover { border-color: #7c3aed; color: #c4b5fd; }
  .muted { color: #52525b; }
  .section-title { font-size: 11px; text-transform: uppercase; letter-spacing: 0.06em; color: #52525b; margin-bottom: 8px; }
  #log   { background: #111115; border: 1px solid #27272a; border-radius: 6px; padding: 12px; height: 200px; overflow-y: auto; font-size: 11px; color: #a1a1aa; line-height: 1.6; }
</style>
</head>
<body>
<h1>tsk-local</h1>
<p class="sub">Local Tickstem dev server — jobs and history reset on restart</p>

<div class="section-title">Jobs</div>
<table id="jobs-table">
  <thead><tr><th>Name</th><th>Schedule</th><th>Endpoint</th><th>Status</th><th>Next run</th><th></th></tr></thead>
  <tbody id="jobs-body"><tr><td colspan="6" class="muted">Loading…</td></tr></tbody>
</table>

<div class="section-title">Recent executions</div>
<table id="exec-table">
  <thead><tr><th>Job</th><th>Status</th><th>Duration</th><th>Time</th><th>Error</th></tr></thead>
  <tbody id="exec-body"><tr><td colspan="5" class="muted">Loading…</td></tr></tbody>
</table>

<div class="section-title">Log</div>
<div id="log"></div>

<script>
const jobNames = {};
let lastExecCount = 0;

function humanTime(iso) {
  if (!iso) return '—';
  const d = Math.round((new Date(iso) - Date.now()) / 1000);
  const abs = Math.abs(d);
  const s = abs < 60 ? abs + 's' : abs < 3600 ? Math.floor(abs/60) + 'm' : Math.floor(abs/3600) + 'h';
  return d > 0 ? 'in ' + s : s + ' ago';
}

function tag(text, cls) {
  return '<span class="tag ' + cls + '">' + text + '</span>';
}

async function trigger(jobId, jobName) {
  await fetch('/v1/jobs/' + jobId + '/trigger', { method: 'POST' });
  appendLog('▶ manual trigger: ' + jobName);
}

function appendLog(msg) {
  const log = document.getElementById('log');
  const t = new Date().toLocaleTimeString();
  log.innerHTML += '<div>[' + t + '] ' + msg + '</div>';
  log.scrollTop = log.scrollHeight;
}

async function refresh() {
  const jobs = await fetch('/v1/jobs').then(r => r.json()).catch(() => []);

  // rebuild jobNames index
  jobs.forEach(j => { jobNames[j.id] = j.name; });

  const tbody = document.getElementById('jobs-body');
  if (jobs.length === 0) {
    tbody.innerHTML = '<tr><td colspan="6" class="muted">No jobs registered yet.</td></tr>';
  } else {
    tbody.innerHTML = jobs.map(function(j) {
    return '<tr>' +
      '<td>' + j.name + '</td>' +
      '<td>' + j.schedule + '</td>' +
      '<td class="muted">' + j.endpoint + '</td>' +
      '<td>' + tag(j.status, j.status) + '</td>' +
      '<td class="muted">' + humanTime(j.next_run_at) + '</td>' +
      '<td><button onclick="trigger(\'' + j.id + '\',\'' + j.name + '\')">run</button></td>' +
      '</tr>';
  }).join('');
  }

  // collect all executions across all jobs
  const allExecs = [];
  for (const job of jobs) {
    const execs = await fetch('/v1/jobs/' + job.id + '/executions').then(r => r.json()).catch(() => []);
    execs.forEach(e => allExecs.push(e));
  }
  allExecs.sort((a, b) => new Date(b.scheduled_at) - new Date(a.scheduled_at));
  const recent = allExecs.slice(0, 20);

  if (recent.length > lastExecCount) {
    recent.slice(0, recent.length - lastExecCount).forEach(e => {
      const name = jobNames[e.job_id] || e.job_id;
      if (e.status === 'success') appendLog('✓ ' + name + ' — ' + e.duration_ms + 'ms');
      else if (e.status === 'failed') appendLog('✗ ' + name + ' — ' + (e.error || 'failed'));
    });
    lastExecCount = recent.length;
  }

  const ebdy = document.getElementById('exec-body');
  if (recent.length === 0) {
    ebdy.innerHTML = '<tr><td colspan="5" class="muted">No executions yet.</td></tr>';
  } else {
    ebdy.innerHTML = recent.map(function(e) {
    return '<tr>' +
      '<td>' + (jobNames[e.job_id] || e.job_id) + '</td>' +
      '<td>' + tag(e.status, e.status) + '</td>' +
      '<td class="muted">' + (e.duration_ms != null ? e.duration_ms + 'ms' : '—') + '</td>' +
      '<td class="muted">' + humanTime(e.finished_at || e.scheduled_at) + '</td>' +
      '<td class="muted">' + (e.error || '') + '</td>' +
      '</tr>';
  }).join('');
  }
}

refresh();
setInterval(refresh, 2000);
</script>
</body>
</html>`

