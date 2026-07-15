package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(url string) *Client {
	return &Client{
		BaseURL:    url,
		Model:      "test-model",
		APIKey:     "sk-test",
		MaxRetries: 3,
		Timeout:    5 * time.Second,
	}
}

func TestCompleteSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("auth header = %q", got)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"findings\":[]}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL + "/v1")
	content, usage, err := c.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatal(err)
	}
	if content != `{"findings":[]}` {
		t.Errorf("content = %q", content)
	}
	if usage.TotalTokens != 15 {
		t.Errorf("usage total = %d, want 15", usage.TotalTokens)
	}
}

func TestCompleteSendsMaxTokens(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL + "/v1")
	c.MaxTokens = 512
	if _, _, err := c.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if body["max_tokens"] != float64(512) {
		t.Errorf("max_tokens in request = %v, want 512", body["max_tokens"])
	}
}

func TestCompleteRetriesOn429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}],"usage":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(srv.URL + "/v1")
	c.MaxRetries = 2
	if _, _, err := c.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", calls.Load())
	}
}

func TestCompleteNonRetryable(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL + "/v1")
	if _, _, err := c.Complete(context.Background(), "s", "u"); err == nil {
		t.Fatal("expected error on HTTP 400")
	}
	if calls.Load() != 1 {
		t.Errorf("400 must not be retried; got %d calls", calls.Load())
	}
}

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		in, want string
		wantErr  bool
	}{
		{`{"findings":[]}`, `{"findings":[]}`, false},
		{"```json\n{\"a\":1}\n```", `{"a":1}`, false},
		{"Here you go:\n{\"a\":1}\nDone.", `{"a":1}`, false},
		{"no json here", "", true},
	}
	for _, tc := range cases {
		got, err := ExtractJSON(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ExtractJSON(%q) err=%v, wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("ExtractJSON(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
