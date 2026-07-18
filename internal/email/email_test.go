package email

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSenderValidation(t *testing.T) {
	if _, err := NewSender(Config{Provider: "buttondown", APIKey: ""}); err == nil {
		t.Error("empty API key should error")
	}
	if _, err := NewSender(Config{Provider: "resend", APIKey: "k"}); err == nil {
		t.Error("resend without From/AudienceID should error")
	}
	if _, err := NewSender(Config{Provider: "wat", APIKey: "k"}); err == nil {
		t.Error("unknown provider should error")
	}
	if s, err := NewSender(Config{APIKey: "k"}); err != nil || s.Name() != "buttondown" {
		t.Errorf("empty provider should default to buttondown, got %v (%v)", s, err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	env := map[string]string{}
	if _, ok := ConfigFromEnv(func(k string) string { return env[k] }); ok {
		t.Error("no EMAIL_API_KEY should give ok=false")
	}
	env["EMAIL_API_KEY"] = "secret"
	env["EMAIL_PROVIDER"] = "resend"
	cfg, ok := ConfigFromEnv(func(k string) string { return env[k] })
	if !ok || cfg.APIKey != "secret" || cfg.Provider != "resend" {
		t.Errorf("unexpected config: %+v ok=%v", cfg, ok)
	}
}

func TestButtondownSubscribe(t *testing.T) {
	var gotAuth, gotPath, gotEmail string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		gotEmail, _ = body["email_address"].(string)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	s, err := NewSender(Config{Provider: "buttondown", APIKey: "k123", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Subscribe(context.Background(), "a@b.com"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if gotAuth != "Token k123" {
		t.Errorf("auth = %q, want %q", gotAuth, "Token k123")
	}
	if gotPath != "/v1/subscribers" {
		t.Errorf("path = %q", gotPath)
	}
	if gotEmail != "a@b.com" {
		t.Errorf("email = %q", gotEmail)
	}
}

func TestButtondownSubscribeAlreadyExistsIsOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"This email address is already subscribed."}`))
	}))
	defer srv.Close()
	s, _ := NewSender(Config{APIKey: "k", BaseURL: srv.URL})
	if err := s.Subscribe(context.Background(), "dup@b.com"); err != nil {
		t.Errorf("already-subscribed should be treated as success, got %v", err)
	}
}

func TestButtondownSubscribeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"bad token"}`))
	}))
	defer srv.Close()
	s, _ := NewSender(Config{APIKey: "k", BaseURL: srv.URL})
	if err := s.Subscribe(context.Background(), "x@b.com"); err == nil {
		t.Error("401 should return an error")
	}
}

func TestButtondownBroadcast(t *testing.T) {
	var gotPath, gotSubject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		gotSubject, _ = body["subject"].(string)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	s, _ := NewSender(Config{APIKey: "k", BaseURL: srv.URL})
	if err := s.Broadcast(context.Background(), "Weekly", "<h1>hi</h1>"); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	if gotPath != "/v1/emails" || gotSubject != "Weekly" {
		t.Errorf("path=%q subject=%q", gotPath, gotSubject)
	}
}

func TestResendBroadcastCreatesThenSends(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer rk" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if strings.HasSuffix(r.URL.Path, "/send") {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"bc_1"}`))
	}))
	defer srv.Close()
	s, err := NewSender(Config{Provider: "resend", APIKey: "rk", From: "n@x.com", AudienceID: "aud_1", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Broadcast(context.Background(), "Weekly", "<p>hi</p>"); err != nil {
		t.Fatalf("broadcast: %v", err)
	}
	want := []string{"POST /broadcasts", "POST /broadcasts/bc_1/send"}
	if len(paths) != 2 || paths[0] != want[0] || paths[1] != want[1] {
		t.Errorf("calls = %v, want %v", paths, want)
	}
}
