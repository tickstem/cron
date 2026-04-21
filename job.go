package cron

import "time"

type Job struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Schedule    string     `json:"schedule"`
	Endpoint    string     `json:"endpoint"`
	Method      string     `json:"method"`
	TimeoutSecs int        `json:"timeout_secs"`
	Status      string     `json:"status"`
	NextRunAt   *time.Time `json:"next_run_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type RegisterParams struct {
	Name        string `json:"name"`
	Schedule    string `json:"schedule"`
	Endpoint    string `json:"endpoint"`
	Description string `json:"description,omitempty"`
	// Method defaults to "POST" when empty.
	Method string `json:"method,omitempty"`
	// Headers are sent with each request — use for endpoint authentication.
	Headers map[string]string `json:"headers,omitempty"`
	// TimeoutSecs defaults to 30 when zero.
	TimeoutSecs int `json:"timeout_secs,omitempty"`
}

type ExecutionStatus string

const (
	ExecutionStatusSuccess ExecutionStatus = "success"
	ExecutionStatusFailed  ExecutionStatus = "failed"
)

type Execution struct {
	ID          string          `json:"id"`
	JobID       string          `json:"job_id"`
	Status      ExecutionStatus `json:"status"`
	StatusCode  *int            `json:"status_code,omitempty"`
	DurationMs  *int64          `json:"duration_ms,omitempty"`
	Error       string          `json:"error,omitempty"`
	ScheduledAt time.Time       `json:"scheduled_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	FinishedAt  *time.Time      `json:"finished_at,omitempty"`
}
