// Package cron provides a Go client for the Tickstem cron job API.
//
// Usage:
//
//	client := cron.New(os.Getenv("TICKSTEM_API_KEY"))
//
//	job, err := client.Register(ctx, cron.RegisterParams{
//	    Name:     "daily-cleanup",
//	    Schedule: "0 2 * * *",
//	    Endpoint: "https://yourapp.com/jobs/cleanup",
//	})
package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.tickstem.dev/v1"

// Client is a Tickstem API client. Create one with New.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL. Useful for testing with tsk-local.
func WithBaseURL(url string) Option {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithHTTPClient replaces the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// New creates a Client authenticated with apiKey.
// Options are applied in order after the defaults are set.
func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Register creates a new cron job and returns it.
func (c *Client) Register(ctx context.Context, params RegisterParams) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodPost, "/jobs", params, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// List returns all jobs for the authenticated account.
func (c *Client) List(ctx context.Context) ([]Job, error) {
	var result struct {
		Jobs []Job `json:"jobs"`
	}
	if err := c.do(ctx, http.MethodGet, "/jobs", nil, &result); err != nil {
		return nil, err
	}
	return result.Jobs, nil
}

// Get returns a single job by ID.
func (c *Client) Get(ctx context.Context, jobID string) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodGet, "/jobs/"+jobID, nil, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// Pause suspends a job so it no longer fires.
func (c *Client) Pause(ctx context.Context, jobID string) (*Job, error) {
	return c.setStatus(ctx, jobID, "paused")
}

// Resume reactivates a paused job.
func (c *Client) Resume(ctx context.Context, jobID string) (*Job, error) {
	return c.setStatus(ctx, jobID, "active")
}

// Update replaces a job's configuration. All RegisterParams fields are overwritten.
func (c *Client) Update(ctx context.Context, jobID string, params RegisterParams) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodPut, "/jobs/"+jobID, params, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// Delete permanently removes a job and its execution history.
func (c *Client) Delete(ctx context.Context, jobID string) error {
	return c.do(ctx, http.MethodDelete, "/jobs/"+jobID, nil, nil)
}

// Executions returns the execution history for a job, most recent first.
func (c *Client) Executions(ctx context.Context, jobID string) ([]Execution, error) {
	var result struct {
		Executions []Execution `json:"executions"`
	}
	if err := c.do(ctx, http.MethodGet, "/executions?job_id="+jobID, nil, &result); err != nil {
		return nil, err
	}
	return result.Executions, nil
}

// ── internal ──────────────────────────────────────────────────────────────────

type statusBody struct {
	Status string `json:"status"`
}

func (c *Client) setStatus(ctx context.Context, jobID, status string) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodPatch, "/jobs/"+jobID, statusBody{Status: status}, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	req, err := c.buildRequest(ctx, method, path, body)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tickstem: request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("tickstem: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, responseBody)
	}

	if out != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, out); err != nil {
			return fmt.Errorf("tickstem: decode response: %w", err)
		}
	}

	return nil
}

func (c *Client) buildRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("tickstem: encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("tickstem: build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	return req, nil
}
