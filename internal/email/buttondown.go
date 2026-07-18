package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// buttondown implements Sender against the Buttondown API. Buttondown is
// newsletter-native: it owns the subscriber list, double opt-in, unsubscribe
// links and compliance.
type buttondown struct {
	apiKey string
	base   string
	client *http.Client
}

func newButtondown(cfg Config, client *http.Client) *buttondown {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.buttondown.email"
	}
	return &buttondown{apiKey: cfg.APIKey, base: strings.TrimRight(base, "/"), client: client}
}

func (b *buttondown) Name() string { return "buttondown" }

func (b *buttondown) Subscribe(ctx context.Context, email string) error {
	status, body, err := b.do(ctx, http.MethodPost, "/v1/subscribers", map[string]any{
		"email_address": email,
	})
	if err != nil {
		return err
	}
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusBadRequest && strings.Contains(strings.ToLower(body), "already"):
		return nil // already subscribed — idempotent
	default:
		return fmt.Errorf("buttondown: subscribe failed (%d): %s", status, body)
	}
}

func (b *buttondown) Broadcast(ctx context.Context, subject, htmlBody string) error {
	status, body, err := b.do(ctx, http.MethodPost, "/v1/emails", map[string]any{
		"subject": subject,
		"body":    htmlBody,
		"status":  "about_to_send",
	})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("buttondown: broadcast failed (%d): %s", status, body)
	}
	return nil
}

func (b *buttondown) do(ctx context.Context, method, path string, payload any) (int, string, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("buttondown: marshaling request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, b.base+path, bytes.NewReader(buf))
	if err != nil {
		return 0, "", fmt.Errorf("buttondown: building request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+b.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("buttondown: request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, string(respBody), nil
}
