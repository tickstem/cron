package main

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	tsk "github.com/tickstem/cron"
)

type scheduler struct {
	cron    *cron.Cron
	entries map[string]cron.EntryID // job ID → cron entry ID
	runJob  func(job tsk.Job)
}

func newScheduler(runJob func(job tsk.Job)) *scheduler {
	return &scheduler{
		cron:    cron.New(cron.WithSeconds()),
		entries: make(map[string]cron.EntryID),
		runJob:  runJob,
	}
}

func (s *scheduler) start() {
	s.cron.Start()
}

func (s *scheduler) stop() {
	s.cron.Stop()
}

func (s *scheduler) addJob(job tsk.Job) (int, time.Time, error) {
	// Prefix with "0 " to allow standard 5-field cron (min hour dom mon dow).
	// robfig/cron/v3 with WithSeconds() expects 6 fields; we normalise here.
	schedule := "0 " + job.Schedule

	entryID, err := s.cron.AddFunc(schedule, func() {
		s.runJob(job)
	})
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("parse schedule %q: %w", job.Schedule, err)
	}

	s.entries[job.ID] = entryID
	nextRun := s.cron.Entry(entryID).Next
	return int(entryID), nextRun, nil
}

func (s *scheduler) removeJob(entryID int) {
	s.cron.Remove(cron.EntryID(entryID))
}

func (s *scheduler) nextRun(jobID string) (time.Time, bool) {
	entryID, ok := s.entries[jobID]
	if !ok {
		return time.Time{}, false
	}
	entry := s.cron.Entry(entryID)
	if entry.ID == 0 {
		return time.Time{}, false
	}
	return entry.Next, true
}
