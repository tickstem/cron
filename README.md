# tickstem/cron

[![Go Reference](https://pkg.go.dev/badge/github.com/tickstem/cron.svg)](https://pkg.go.dev/github.com/tickstem/cron)
[![Go Report Card](https://goreportcard.com/badge/github.com/tickstem/cron)](https://goreportcard.com/report/github.com/tickstem/cron)
[![codecov](https://codecov.io/gh/tickstem/cron/badge.svg)](https://codecov.io/gh/tickstem/cron)

Go SDK for [Tickstem](https://tickstem.dev) — reliable cron jobs for production apps.

Works on Vercel, Railway, Render, Fly.io, and anywhere else that can't run an always-on scheduler.

## Install

```bash
go get github.com/tickstem/cron
```

## Quick start

```go
package main

import (
    "context"
    "log"
    "os"

    "github.com/tickstem/cron"
)

func main() {
    client := cron.New(os.Getenv("TICKSTEM_API_KEY"))

    job, err := client.Register(context.Background(), cron.RegisterParams{
        Name:     "daily-cleanup",
        Schedule: "0 2 * * *",
        Endpoint: "https://yourapp.com/jobs/cleanup",
    })
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("job registered: %s (next run: %s)", job.ID, job.NextRunAt)
}
```

Get your API key at [app.tickstem.dev](https://app.tickstem.dev).

## Usage

### Create a client

```go
// Minimal — uses https://api.tickstem.dev/v1
client := cron.New(os.Getenv("TICKSTEM_API_KEY"))

// With options
client := cron.New(apiKey,
    cron.WithBaseURL("http://localhost:8080/v1"), // local dev / tsk-local
)
```

### Register a job

```go
job, err := client.Register(ctx, cron.RegisterParams{
    Name:        "send-digest",
    Description: "Weekly email digest to all users",
    Schedule:    "0 9 * * 1",                         // every Monday at 09:00
    Endpoint:    "https://yourapp.com/jobs/digest",
    Method:      "POST",                               // default
    TimeoutSecs: 60,                                   // default: 30
})
```

### List jobs

```go
jobs, err := client.List(ctx)
for _, j := range jobs {
    fmt.Printf("%s  %s  %s\n", j.ID, j.Schedule, j.Status)
}
```

### Get a job

```go
job, err := client.Get(ctx, "job_abc123")
```

### Pause / Resume

```go
job, err := client.Pause(ctx, jobID)
job, err := client.Resume(ctx, jobID)
```

### Delete a job

```go
err := client.Delete(ctx, jobID)
```

### Execution history

```go
executions, err := client.Executions(ctx, jobID)
for _, e := range executions {
    fmt.Printf("%s  %s  %dms\n", e.ID, e.Status, *e.DurationMs)
}
```

## Securing your endpoint

Tickstem calls your endpoint over the public internet. To prevent unauthorized
callers from triggering your job handler, add a shared secret in the job headers
and validate it in your handler:

```go
job, err := client.Register(ctx, cron.RegisterParams{
    Name:     "daily-cleanup",
    Schedule: "0 2 * * *",
    Endpoint: "https://yourapp.com/jobs/cleanup",
    Method:   "POST",
    Headers: map[string]string{
        "X-Tickstem-Secret": os.Getenv("CRON_SECRET"),
    },
})
```

In your handler, reject requests that don't carry the secret:

```go
func cronHandler(w http.ResponseWriter, r *http.Request) {
    if r.Header.Get("X-Tickstem-Secret") != os.Getenv("CRON_SECRET") {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    // ... do work
}
```

Use a long random value for `CRON_SECRET` (e.g. `openssl rand -hex 32`).
Store it as an environment variable in both your app and your Tickstem job — never hardcode it.

## Error handling

```go
job, err := client.Get(ctx, someID)
if err != nil {
    if cron.IsNotFound(err) {
        // job doesn't exist
    }
    if cron.IsUnauthorized(err) {
        // invalid API key
    }
    if cron.IsQuotaExceeded(err) {
        // monthly execution quota hit — upgrade at app.tickstem.dev/dashboard/billing
    }
    // inspect the raw error
    var apiErr *cron.APIError
    if errors.As(err, &apiErr) {
        fmt.Println(apiErr.StatusCode, apiErr.Message)
    }
}
```

## Local development

Use `tsk-local` to test your job handlers without hitting the live API.
It runs the full Tickstem API contract in-memory on your machine.

```bash
go install github.com/tickstem/cron/cmd/tsk-local@latest
tsk-local                  # starts on :8090
tsk-local --port 9000      # custom port
```

Then point the SDK at it:

```go
client := cron.New("any-key",
    cron.WithBaseURL("http://localhost:8090/v1"))
```

The dashboard at `http://localhost:8090` shows registered jobs,
execution history, and a **run** button for instant manual triggers.

> Jobs and history are in-memory — everything resets on restart.
> Use the real platform for production.

## Cron expression reference

```
┌─────── minute        (0–59)
│ ┌───── hour          (0–23)
│ │ ┌─── day of month  (1–31)
│ │ │ ┌─ month         (1–12)
│ │ │ │ ┌ day of week  (0–6, Sun=0)
│ │ │ │ │
* * * * *

Examples:
  0 * * * *      every hour
  */15 * * * *   every 15 minutes
  0 2 * * *      daily at 02:00 UTC
  0 9 * * 1      every Monday at 09:00 UTC
  0 0 1 * *      first day of every month
```

Use [crontab.guru](https://crontab.guru) to build and validate expressions.

## Pricing

| Plan    | Executions/month | Price  |
|---------|-----------------|--------|
| Free    | 1,000           | $0     |
| Starter | 10,000          | $12/mo |
| Pro     | 100,000         | $29/mo |

[View full pricing →](https://tickstem.dev/#pricing)

## License

MIT
