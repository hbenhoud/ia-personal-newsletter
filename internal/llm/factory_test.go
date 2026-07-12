package llm

import (
	"os"
	"strings"
	"testing"
)

// AC-7 + AC-10: Missing API key produces a clear error (not a panic)

func TestNewProvider_MissingAPIKey(t *testing.T) {
	envVar := "TEST_MISSING_LLM_KEY_XYZ"
	os.Unsetenv(envVar)

	_, err := NewProvider("groq", "llama-3.3-70b-versatile", envVar)
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
	if !strings.Contains(err.Error(), envVar) {
		t.Errorf("error should mention the env var name, got: %v", err)
	}
}

// AC-7: Unknown provider returns a clear error

func TestNewProvider_UnknownProvider(t *testing.T) {
	os.Setenv("DUMMY_KEY", "test")
	defer os.Unsetenv("DUMMY_KEY")

	_, err := NewProvider("unknown-provider", "", "DUMMY_KEY")
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention 'unknown', got: %v", err)
	}
}

// AC-7: Supported providers are instantiated without error

func TestNewProvider_Groq(t *testing.T) {
	os.Setenv("TEST_GROQ_KEY", "test-key")
	defer os.Unsetenv("TEST_GROQ_KEY")

	p, err := NewProvider("groq", "llama-3.3-70b-versatile", "TEST_GROQ_KEY")
	if err != nil {
		t.Fatalf("NewProvider groq: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if !strings.Contains(p.Name(), "groq") {
		t.Errorf("provider name should contain 'groq', got %q", p.Name())
	}
}

func TestNewProvider_Ollama(t *testing.T) {
	os.Setenv("TEST_OLLAMA_KEY", "test-key")
	defer os.Unsetenv("TEST_OLLAMA_KEY")

	p, err := NewProvider("ollama", "", "TEST_OLLAMA_KEY")
	if err != nil {
		t.Fatalf("NewProvider ollama: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if !strings.Contains(p.Name(), "ollama") {
		t.Errorf("provider name should contain 'ollama', got %q", p.Name())
	}
}

func TestNewProvider_Gemini(t *testing.T) {
	os.Setenv("TEST_GEMINI_KEY", "test-key")
	defer os.Unsetenv("TEST_GEMINI_KEY")

	p, err := NewProvider("gemini", "", "TEST_GEMINI_KEY")
	if err != nil {
		t.Fatalf("NewProvider gemini: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if !strings.Contains(p.Name(), "gemini") {
		t.Errorf("provider name should contain 'gemini', got %q", p.Name())
	}
}
