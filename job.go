package cron

import "time"

// Job represents a scheduled cron job.
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

// RegisterParams holds the fields required to create a new job.
type RegisterParams struct {
	// Name is a human-readable label for the job (required).
	Name string `json:"name"`

	// Schedule is a standard 5-field cron expression (required).
	// Example: "0 * * * *" runs every hour at minute 0.
	Schedule string `json:"schedule"`

	// Endpoint is the URL that will be called on each execution (required).
	Endpoint string `json:"endpoint"`

	// Description is an optional human-readable note.
	Description string `json:"description,omitempty"`

	// Method is the HTTP method used when calling Endpoint.
	// Defaults to "POST" when empty.
	Method string `json:"method,omitempty"`

	// TimeoutSecs is how long the executor waits before timing out.
	// Defaults to 30 when zero.
	TimeoutSecs int `json:"timeout_secs,omitempty"`
}

// ExecutionStatus represents the outcome of a single job run.
type ExecutionStatus string

const (
	ExecutionStatusPending ExecutionStatus = "pending"
	ExecutionStatusRunning ExecutionStatus = "running"
	ExecutionStatusSuccess ExecutionStatus = "success"
	ExecutionStatusFailed  ExecutionStatus = "failed"
	ExecutionStatusTimeout ExecutionStatus = "timeout"
)

// Execution is a single run of a Job.
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
