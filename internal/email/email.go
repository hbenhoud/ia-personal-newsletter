// Package email is the subscription/delivery layer for the dynamic product.
// It mirrors the llm.Provider / embedding.Embedder pattern: one interface, one
// implementation per managed provider, switched by config. The provider owns
// the subscriber list (double opt-in, unsubscribe, compliance) — we only add
// subscribers and trigger broadcasts.
package email

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Sender adds subscribers and broadcasts editions.
type Sender interface {
	// Subscribe registers an email with the provider, which handles the
	// double opt-in confirmation. Subscribing an already-known address is not
	// an error.
	Subscribe(ctx context.Context, email string) error
	// Broadcast sends an email to the whole list.
	Broadcast(ctx context.Context, subject, htmlBody string) error
	// Name identifies the provider (for logs).
	Name() string
}

// Config selects and configures a provider.
type Config struct {
	Provider   string // "buttondown" | "resend"
	APIKey     string
	From       string // sender address (resend)
	AudienceID string // list id (resend)
	BaseURL    string // override the API base (tests); empty = provider default
}

// NewSender builds the configured Sender. It returns an error when the provider
// is unknown or required settings are missing.
func NewSender(cfg Config) (Sender, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("email: API key is empty")
	}
	client := &http.Client{Timeout: 15 * time.Second}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "buttondown":
		return newButtondown(cfg, client), nil
	case "resend":
		if cfg.From == "" || cfg.AudienceID == "" {
			return nil, fmt.Errorf("email: resend requires From and AudienceID")
		}
		return newResend(cfg, client), nil
	default:
		return nil, fmt.Errorf("email: unknown provider %q (supported: buttondown, resend)", cfg.Provider)
	}
}

// ConfigFromEnv builds a Config from environment variables:
//
//	EMAIL_PROVIDER  (default "buttondown")
//	EMAIL_API_KEY
//	EMAIL_FROM        (resend)
//	EMAIL_AUDIENCE_ID (resend)
//
// It returns ok=false when EMAIL_API_KEY is unset, so callers can run without
// email configured.
func ConfigFromEnv(getenv func(string) string) (Config, bool) {
	key := strings.TrimSpace(getenv("EMAIL_API_KEY"))
	if key == "" {
		return Config{}, false
	}
	return Config{
		Provider:   getenv("EMAIL_PROVIDER"),
		APIKey:     key,
		From:       getenv("EMAIL_FROM"),
		AudienceID: getenv("EMAIL_AUDIENCE_ID"),
		BaseURL:    getenv("EMAIL_BASE_URL"), // override the API base (local mock / self-host)
	}, true
}
