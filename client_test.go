package cron_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tickstem/cron"
)

// serverFunc is an http.HandlerFunc that also records the incoming request
// for assertion in tests.
type serverFunc func(w http.ResponseWriter, r *http.Request)

func newTestClient(t *testing.T, handler serverFunc) (*cron.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	client := cron.New("tsk_test_key", cron.WithBaseURL(srv.URL))
	return client, srv
}

func writeJSON(t *testing.T, w http.ResponseWriter, statusCode int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

// ── Register ──────────────────────────────────────────────────────────────────

func TestRegister(t *testing.T) {
	t.Run("given valid params when registering then returns created job", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Second)
		want := cron.Job{
			ID:          "job_123",
			Name:        "sync-users",
			Schedule:    "0 * * * *",
			Endpoint:    "https://example.com/sync",
			Method:      "POST",
			TimeoutSecs: 30,
			Status:      "active",
			CreatedAt:   now,
		}

		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "/jobs", r.URL.Path)
			assert.Equal(t, "Bearer tsk_test_key", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var params cron.RegisterParams
			require.NoError(t, json.NewDecoder(r.Body).Decode(&params))
			assert.Equal(t, "sync-users", params.Name)
			assert.Equal(t, "0 * * * *", params.Schedule)

			writeJSON(t, w, http.StatusCreated, want)
		})

		got, err := client.Register(context.Background(), cron.RegisterParams{
			Name:     "sync-users",
			Schedule: "0 * * * *",
			Endpoint: "https://example.com/sync",
		})

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "job_123", got.ID)
		assert.Equal(t, "sync-users", got.Name)
		assert.Equal(t, "active", got.Status)
	})

	t.Run("given API error when registering then returns APIError", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusUnprocessableEntity, map[string]string{
				"error": "invalid cron expression",
			})
		})

		got, err := client.Register(context.Background(), cron.RegisterParams{
			Name:     "bad-job",
			Schedule: "not-a-cron",
			Endpoint: "https://example.com/x",
		})

		require.Error(t, err)
		assert.Nil(t, got)
		var apiErr *cron.APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
		assert.Contains(t, apiErr.Message, "invalid cron expression")
	})

	t.Run("given quota exceeded when registering then IsQuotaExceeded returns true", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusTooManyRequests, map[string]string{
				"error": "monthly quota exceeded",
			})
		})

		_, err := client.Register(context.Background(), cron.RegisterParams{
			Name:     "x",
			Schedule: "* * * * *",
			Endpoint: "https://example.com/x",
		})

		require.Error(t, err)
		assert.True(t, cron.IsQuotaExceeded(err))
	})
}

// ── List ──────────────────────────────────────────────────────────────────────

func TestList(t *testing.T) {
	t.Run("given existing jobs when listing then returns all jobs", func(t *testing.T) {
		jobs := []cron.Job{
			{ID: "job_1", Name: "first", Schedule: "0 * * * *", Status: "active"},
			{ID: "job_2", Name: "second", Schedule: "0 2 * * *", Status: "paused"},
		}

		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/jobs", r.URL.Path)
			writeJSON(t, w, http.StatusOK, jobs)
		})

		got, err := client.List(context.Background())

		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "job_1", got[0].ID)
		assert.Equal(t, "job_2", got[1].ID)
	})

	t.Run("given no jobs when listing then returns empty slice", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusOK, []cron.Job{})
		})

		got, err := client.List(context.Background())

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

// ── Get ───────────────────────────────────────────────────────────────────────

func TestGet(t *testing.T) {
	t.Run("given existing job when getting by ID then returns job", func(t *testing.T) {
		want := cron.Job{ID: "job_abc", Name: "backup", Status: "active"}

		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/jobs/job_abc", r.URL.Path)
			writeJSON(t, w, http.StatusOK, want)
		})

		got, err := client.Get(context.Background(), "job_abc")

		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "job_abc", got.ID)
	})

	t.Run("given unknown job ID when getting then IsNotFound returns true", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
		})

		got, err := client.Get(context.Background(), "job_missing")

		require.Error(t, err)
		assert.Nil(t, got)
		assert.True(t, cron.IsNotFound(err))
	})
}

// ── Pause / Resume ────────────────────────────────────────────────────────────

func TestPause(t *testing.T) {
	t.Run("given active job when pausing then sends PATCH with paused status", func(t *testing.T) {
		want := cron.Job{ID: "job_1", Status: "paused"}

		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPatch, r.Method)
			assert.Equal(t, "/jobs/job_1", r.URL.Path)

			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "paused", body["status"])

			writeJSON(t, w, http.StatusOK, want)
		})

		got, err := client.Pause(context.Background(), "job_1")

		require.NoError(t, err)
		assert.Equal(t, "paused", got.Status)
	})
}

func TestResume(t *testing.T) {
	t.Run("given paused job when resuming then sends PATCH with active status", func(t *testing.T) {
		want := cron.Job{ID: "job_1", Status: "active"}

		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "active", body["status"])
			writeJSON(t, w, http.StatusOK, want)
		})

		got, err := client.Resume(context.Background(), "job_1")

		require.NoError(t, err)
		assert.Equal(t, "active", got.Status)
	})
}

// ── Delete ────────────────────────────────────────────────────────────────────

func TestDelete(t *testing.T) {
	t.Run("given existing job when deleting then sends DELETE and returns no error", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodDelete, r.Method)
			assert.Equal(t, "/jobs/job_del", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		})

		err := client.Delete(context.Background(), "job_del")

		require.NoError(t, err)
	})

	t.Run("given invalid API key when deleting then IsUnauthorized returns true", func(t *testing.T) {
		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(t, w, http.StatusUnauthorized, map[string]string{"error": "invalid API key"})
		})

		err := client.Delete(context.Background(), "job_x")

		require.Error(t, err)
		assert.True(t, cron.IsUnauthorized(err))
	})
}

// ── Executions ────────────────────────────────────────────────────────────────

func TestExecutions(t *testing.T) {
	t.Run("given job with history when listing executions then returns executions", func(t *testing.T) {
		code := 200
		duration := int64(342)
		executions := []cron.Execution{
			{
				ID:         "exec_1",
				JobID:      "job_1",
				Status:     cron.ExecutionStatusSuccess,
				StatusCode: &code,
				DurationMs: &duration,
			},
		}

		client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/jobs/job_1/executions", r.URL.Path)
			writeJSON(t, w, http.StatusOK, executions)
		})

		got, err := client.Executions(context.Background(), "job_1")

		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, cron.ExecutionStatusSuccess, got[0].Status)
		assert.Equal(t, 200, *got[0].StatusCode)
		assert.Equal(t, int64(342), *got[0].DurationMs)
	})
}

// ── Options ───────────────────────────────────────────────────────────────────

func TestWithBaseURL(t *testing.T) {
	t.Run("given custom base URL when making request then uses that URL", func(t *testing.T) {
		requestReceived := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestReceived = true
			writeJSON(t, w, http.StatusOK, []cron.Job{})
		}))
		defer srv.Close()

		client := cron.New("tsk_key", cron.WithBaseURL(srv.URL))
		_, err := client.List(context.Background())

		require.NoError(t, err)
		assert.True(t, requestReceived)
	})
}
