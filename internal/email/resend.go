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

// resend implements Sender against the Resend API using Audiences/Contacts for
// the list and Broadcasts for sending.
type resend struct {
	apiKey     string
	from       string
	audienceID string
	base       string
	client     *http.Client
}

func newResend(cfg Config, client *http.Client) *resend {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.resend.com"
	}
	return &resend{
		apiKey:     cfg.APIKey,
		from:       cfg.From,
		audienceID: cfg.AudienceID,
		base:       strings.TrimRight(base, "/"),
		client:     client,
	}
}

func (r *resend) Name() string { return "resend" }

func (r *resend) Subscribe(ctx context.Context, email string) error {
	status, body, _, err := r.do(ctx, http.MethodPost,
		"/audiences/"+r.audienceID+"/contacts",
		map[string]any{"email": email, "unsubscribed": false})
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	// Resend returns 409-ish / validation errors for existing contacts.
	if strings.Contains(strings.ToLower(body), "already") {
		return nil
	}
	return fmt.Errorf("resend: subscribe failed (%d): %s", status, body)
}

func (r *resend) Broadcast(ctx context.Context, subject, htmlBody string) error {
	status, body, created, err := r.do(ctx, http.MethodPost, "/broadcasts", map[string]any{
		"audience_id": r.audienceID,
		"from":        r.from,
		"subject":     subject,
		"html":        htmlBody,
	})
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("resend: create broadcast failed (%d): %s", status, body)
	}
	id, _ := created["id"].(string)
	if id == "" {
		return fmt.Errorf("resend: broadcast response missing id: %s", body)
	}
	status, body, _, err = r.do(ctx, http.MethodPost, "/broadcasts/"+id+"/send", nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("resend: send broadcast failed (%d): %s", status, body)
	}
	return nil
}

func (r *resend) do(ctx context.Context, method, path string, payload any) (int, string, map[string]any, error) {
	var reader io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return 0, "", nil, fmt.Errorf("resend: marshaling request: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.base+path, reader)
	if err != nil {
		return 0, "", nil, fmt.Errorf("resend: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return 0, "", nil, fmt.Errorf("resend: request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	return resp.StatusCode, string(body), parsed, nil
}
