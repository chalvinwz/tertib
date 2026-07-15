// Package notify delivers a run summary to external channels. Notification
// failures are the caller's to log; they must never change the exit code.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// discordLimit is Discord's maximum message content length.
const discordLimit = 2000

// Discord posts content to a Discord webhook URL. Content is truncated to fit
// Discord's message limit.
func Discord(ctx context.Context, webhookURL, content string, timeout time.Duration) error {
	if len(content) > discordLimit {
		content = content[:discordLimit-1] + "…"
	}
	payload, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}

	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return fmt.Errorf("discord webhook returned HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}
