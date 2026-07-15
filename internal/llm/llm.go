// Package llm is a minimal client for OpenAI-compatible chat completion
// endpoints. It has no vendor SDK dependency: any gateway, proxy, or local
// model that speaks the /chat/completions API works.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client calls an OpenAI-compatible endpoint.
type Client struct {
	BaseURL     string // includes the version path, e.g. https://host/v1
	Model       string
	APIKey      string
	Temperature float64
	MaxTokens   int // caps output tokens per request; omitted when zero
	MaxRetries  int
	Timeout     time.Duration
	HTTPClient  *http.Client
}

// Usage reports token consumption for a completion.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Add returns the element-wise sum, for aggregating usage across calls.
func (u Usage) Add(o Usage) Usage {
	return Usage{
		PromptTokens:     u.PromptTokens + o.PromptTokens,
		CompletionTokens: u.CompletionTokens + o.CompletionTokens,
		TotalTokens:      u.TotalTokens + o.TotalTokens,
	}
}

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

// Complete sends a system+user prompt and returns the model's text response.
// It requests a JSON object response and retries transient failures (network
// errors, HTTP 429, and 5xx) with exponential backoff.
func (c *Client) Complete(ctx context.Context, system, user string) (string, Usage, error) {
	payload, err := json.Marshal(chatRequest{
		Model:          c.Model,
		Messages:       []Message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature:    c.Temperature,
		MaxTokens:      c.MaxTokens,
		ResponseFormat: &responseFormat{Type: "json_object"},
	})
	if err != nil {
		return "", Usage{}, err
	}

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
			if backoff > 8*time.Second {
				backoff = 8 * time.Second
			}
			if err := sleep(ctx, backoff); err != nil {
				return "", Usage{}, err
			}
		}
		content, usage, retryable, err := c.do(ctx, payload)
		if err == nil {
			return content, usage, nil
		}
		lastErr = err
		if !retryable {
			return "", Usage{}, err
		}
	}
	return "", Usage{}, fmt.Errorf("after %d retries: %w", c.MaxRetries, lastErr)
}

func (c *Client) do(ctx context.Context, payload []byte) (content string, usage Usage, retryable bool, err error) {
	reqCtx := ctx
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", Usage{}, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", Usage{}, true, err // network/transport errors are retryable
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))

	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return "", Usage{}, retry, fmt.Errorf("endpoint returned HTTP %d: %s", resp.StatusCode, bodySnippet(body))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", Usage{}, false, fmt.Errorf("decode response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", Usage{}, false, errors.New("response contained no choices")
	}
	return cr.Choices[0].Message.Content, cr.Usage, false, nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func bodySnippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ExtractJSON pulls a JSON object out of a model response that may be wrapped
// in prose or markdown code fences. It returns the substring from the first
// "{" to the last "}".
func ExtractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < 0 || end < start {
		return "", errors.New("no JSON object found in model response")
	}
	return s[start : end+1], nil
}
