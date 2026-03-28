package llm

import "context"

// GenerationConfig controls LLM completion behaviour.
type GenerationConfig struct {
	MaxTokens   int
	Temperature float64
	Model       string // overrides the provider default if non-empty
}

// Provider generates text completions from a prompt.
type Provider interface {
	Complete(ctx context.Context, prompt string, cfg GenerationConfig) (string, error)
	// Name returns a human-readable identifier used in logs and CLI output.
	Name() string
}
