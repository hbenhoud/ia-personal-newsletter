package llm

import (
	"fmt"
	"os"
)

// NewProvider creates the configured LLM Provider.
func NewProvider(providerName, model, apiKeyEnv string) (Provider, error) {
	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("environment variable %s is not set", apiKeyEnv)
	}

	switch providerName {
	case "groq":
		return NewGroqProvider(apiKey, model), nil
	case "gemini":
		return NewGeminiProvider(apiKey, model), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (supported: groq, gemini)", providerName)
	}
}
