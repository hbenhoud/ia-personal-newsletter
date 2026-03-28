package embedding

import (
	"fmt"
	"os"
)

// NewEmbedder creates the configured Embedder implementation.
func NewEmbedder(provider, model, apiKeyEnv, cachePath string) (Embedder, *Cache, error) {
	cache, err := NewCache(cachePath)
	if err != nil {
		return nil, nil, fmt.Errorf("loading embedding cache: %w", err)
	}

	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, nil, fmt.Errorf("environment variable %s is not set", apiKeyEnv)
	}

	switch provider {
	case "gemini":
		if model == "" {
			model = "text-embedding-004"
		}
		return NewGeminiEmbedder(apiKey, model, cache), cache, nil
	case "huggingface":
		if model == "" {
			model = "sentence-transformers/all-MiniLM-L6-v2"
		}
		return NewHuggingFaceEmbedder(apiKey, model, cache), cache, nil
	default:
		return nil, nil, fmt.Errorf("unknown embedding provider %q (supported: gemini, huggingface)", provider)
	}
}
