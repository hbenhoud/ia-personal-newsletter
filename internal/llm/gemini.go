package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GeminiProvider struct {
	apiKey       string
	defaultModel string
	client       *http.Client
}

func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-2.5-flash-lite"
	}
	return &GeminiProvider{
		apiKey:       apiKey,
		defaultModel: model,
		client:       &http.Client{},
	}
}

func (g *GeminiProvider) Name() string { return "gemini/" + g.defaultModel }

func (g *GeminiProvider) Complete(ctx context.Context, prompt string, cfg GenerationConfig) (string, error) {
	model := g.defaultModel
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

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, g.apiKey,
	)

	payload, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": maxTokens,
			"temperature":     temperature,
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini HTTP %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parsing gemini response: %w", err)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no content")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}
