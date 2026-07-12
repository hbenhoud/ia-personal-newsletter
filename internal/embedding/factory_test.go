package embedding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AC-7 + AC-10: Missing API key produces a clear error

func TestNewEmbedder_MissingAPIKey(t *testing.T) {
	envVar := "TEST_MISSING_EMBED_KEY_XYZ"
	os.Unsetenv(envVar)

	_, _, err := NewEmbedder("gemini", "text-embedding-004", envVar, filepath.Join(t.TempDir(), "cache.json"))
	if err == nil {
		t.Fatal("expected error for missing API key, got nil")
	}
	if !strings.Contains(err.Error(), envVar) {
		t.Errorf("error should mention the env var name, got: %v", err)
	}
}

// AC-7: Unknown provider returns a clear error

func TestNewEmbedder_UnknownProvider(t *testing.T) {
	os.Setenv("DUMMY_EMBED_KEY", "test")
	defer os.Unsetenv("DUMMY_EMBED_KEY")

	_, _, err := NewEmbedder("unknown-provider", "", "DUMMY_EMBED_KEY", filepath.Join(t.TempDir(), "cache.json"))
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error should mention 'unknown', got: %v", err)
	}
}

// AC-7: Supported providers are instantiated without error

func TestNewEmbedder_Gemini(t *testing.T) {
	os.Setenv("TEST_GEMINI_EMBED_KEY", "test-key")
	defer os.Unsetenv("TEST_GEMINI_EMBED_KEY")

	embedder, cache, err := NewEmbedder("gemini", "text-embedding-004", "TEST_GEMINI_EMBED_KEY", filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatalf("NewEmbedder gemini: %v", err)
	}
	if embedder == nil {
		t.Fatal("expected non-nil embedder")
	}
	if cache == nil {
		t.Fatal("expected non-nil cache")
	}
}

func TestNewEmbedder_HuggingFace(t *testing.T) {
	os.Setenv("TEST_HF_KEY", "test-key")
	defer os.Unsetenv("TEST_HF_KEY")

	embedder, cache, err := NewEmbedder("huggingface", "sentence-transformers/all-MiniLM-L6-v2", "TEST_HF_KEY", filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatalf("NewEmbedder huggingface: %v", err)
	}
	if embedder == nil || cache == nil {
		t.Fatal("expected non-nil embedder and cache")
	}
}

func TestNewEmbedder_Ollama(t *testing.T) {
	os.Setenv("TEST_OLLAMA_EMBED_KEY", "test-key")
	defer os.Unsetenv("TEST_OLLAMA_EMBED_KEY")

	embedder, cache, err := NewEmbedder("ollama", "", "TEST_OLLAMA_EMBED_KEY", filepath.Join(t.TempDir(), "cache.json"))
	if err != nil {
		t.Fatalf("NewEmbedder ollama: %v", err)
	}
	if embedder == nil || cache == nil {
		t.Fatal("expected non-nil embedder and cache")
	}
}

// AC-7: Default models are applied when model is empty

func TestNewEmbedder_DefaultModels(t *testing.T) {
	os.Setenv("TEST_KEY_DEFAULT", "k")
	defer os.Unsetenv("TEST_KEY_DEFAULT")
	cachePath := filepath.Join(t.TempDir(), "c.json")

	// Should not error — default model applied internally
	_, _, err := NewEmbedder("gemini", "", "TEST_KEY_DEFAULT", cachePath)
	if err != nil {
		t.Fatalf("gemini with empty model: %v", err)
	}

	_, _, err = NewEmbedder("huggingface", "", "TEST_KEY_DEFAULT", cachePath)
	if err != nil {
		t.Fatalf("huggingface with empty model: %v", err)
	}

	_, _, err = NewEmbedder("ollama", "", "TEST_KEY_DEFAULT", cachePath)
	if err != nil {
		t.Fatalf("ollama with empty model: %v", err)
	}
}
