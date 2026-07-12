package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OllamaProvider talks to Ollama Cloud's native chat API (https://ollama.com/api/chat).
type OllamaProvider struct {
	apiKey       string
	defaultModel string
	client       *http.Client
}

func NewOllamaProvider(apiKey, model string) *OllamaProvider {
	if model == "" {
		model = "gpt-oss:120b"
	}
	return &OllamaProvider{
		apiKey:       apiKey,
		defaultModel: model,
		client:       &http.Client{},
	}
}

func (o *OllamaProvider) Name() string { return "ollama/" + o.defaultModel }

func (o *OllamaProvider) Complete(ctx context.Context, prompt string, cfg GenerationConfig) (string, error) {
	model := o.defaultModel
	if cfg.Model != "" {
		model = cfg.Model
	}
	maxTokens := 512
	if cfg.MaxTokens > 0 {
		maxTokens = cfg.MaxTokens
	}
	temperature := 0.3
	if cfg.Temperature > 0 {
		temperature = cfg.Temperature
	}

	// think:false disables chain-of-thought on reasoning models (e.g. GLM):
	// otherwise the token budget is spent on "thinking" and content comes back
	// empty. Models without thinking support ignore the flag.
	payload, _ := json.Marshal(map[string]any{
		"model":  model,
		"stream": false,
		"think":  false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"options": map[string]any{
			"temperature": temperature,
			"num_predict": maxTokens,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://ollama.com/api/chat",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama HTTP %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parsing ollama response: %w", err)
	}

	return result.Message.Content, nil
}
