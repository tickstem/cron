package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tickstem/cron"
)

type executionResult struct {
	statusCode int
	durationMs int64
	err        string
}

func executeJob(job cron.Job) executionResult {
	method := job.Method
	if method == "" {
		method = http.MethodPost
	}

	timeout := time.Duration(job.TimeoutSecs) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, job.Endpoint, nil)
	if err != nil {
		return executionResult{err: fmt.Sprintf("build request: %s", err)}
	}
	req.Header.Set("User-Agent", "tsk-local/1.0")
	req.Header.Set("X-Tickstem-Job-ID", job.ID)
	req.Header.Set("X-Tickstem-Job-Name", job.Name)

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	durationMs := time.Since(start).Milliseconds()

	if err != nil {
		return executionResult{durationMs: durationMs, err: err.Error()}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 400 {
		return executionResult{
			statusCode: resp.StatusCode,
			durationMs: durationMs,
			err:        fmt.Sprintf("HTTP %d response from endpoint", resp.StatusCode),
		}
	}

	return executionResult{statusCode: resp.StatusCode, durationMs: durationMs}
}
