package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDiscordPostsContent(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Discord(context.Background(), srv.URL, "hello", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if got["content"] != "hello" {
		t.Errorf("content = %q, want hello", got["content"])
	}
}

func TestDiscordTruncates(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	long := strings.Repeat("x", 5000)
	if err := Discord(context.Background(), srv.URL, long, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if len([]rune(got["content"])) > discordLimit {
		t.Errorf("content length %d exceeds limit %d", len([]rune(got["content"])), discordLimit)
	}
}

func TestDiscordErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	if err := Discord(context.Background(), srv.URL, "x", 5*time.Second); err == nil {
		t.Fatal("expected error on HTTP 400")
	}
}
