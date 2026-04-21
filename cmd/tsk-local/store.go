package main

import (
	"sync"
	"time"

	"github.com/tickstem/cron"
)

type jobRecord struct {
	job         cron.Job
	cronEntryID int
}

type store struct {
	mu         sync.RWMutex
	jobs       map[string]*jobRecord
	executions map[string][]cron.Execution // keyed by job ID, newest first
}

func newStore() *store {
	return &store{
		jobs:       make(map[string]*jobRecord),
		executions: make(map[string][]cron.Execution),
	}
}

func (s *store) addJob(job cron.Job, entryID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.ID] = &jobRecord{job: job, cronEntryID: entryID}
}

func (s *store) getJob(id string) (cron.Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.jobs[id]
	if !ok {
		return cron.Job{}, false
	}
	return rec.job, true
}

func (s *store) listJobs() []cron.Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]cron.Job, 0, len(s.jobs))
	for _, rec := range s.jobs {
		jobs = append(jobs, rec.job)
	}
	return jobs
}

func (s *store) updateJobStatus(id, status string) (cron.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.jobs[id]
	if !ok {
		return cron.Job{}, false
	}
	rec.job.Status = status
	return rec.job, true
}

func (s *store) updateJobNextRun(id string, nextRun time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.jobs[id]
	if !ok {
		return
	}
	rec.job.NextRunAt = &nextRun
}

func (s *store) updateJobParams(id string, params cron.RegisterParams) (cron.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.jobs[id]
	if !ok {
		return cron.Job{}, false
	}
	rec.job.Name = params.Name
	rec.job.Schedule = params.Schedule
	rec.job.Endpoint = params.Endpoint
	rec.job.Description = params.Description
	rec.job.Method = params.Method
	rec.job.TimeoutSecs = params.TimeoutSecs
	return rec.job, true
}

func (s *store) deleteJob(id string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.jobs[id]
	if !ok {
		return 0, false
	}
	entryID := rec.cronEntryID
	delete(s.jobs, id)
	delete(s.executions, id)
	return entryID, true
}

func (s *store) addExecution(exec cron.Execution) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// prepend so newest is first
	s.executions[exec.JobID] = append([]cron.Execution{exec}, s.executions[exec.JobID]...)
}

func (s *store) listExecutions(jobID string) []cron.Execution {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.executions[jobID]
}

func (s *store) updateExecution(exec cron.Execution) {
	s.mu.Lock()
	defer s.mu.Unlock()
	execs := s.executions[exec.JobID]
	for i, e := range execs {
		if e.ID == exec.ID {
			execs[i] = exec
			return
		}
	}
}
