package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type GeminiEmbedder struct {
	apiKey string
	model  string
	cache  *Cache
	client *http.Client
}

func NewGeminiEmbedder(apiKey, model string, cache *Cache) *GeminiEmbedder {
	return &GeminiEmbedder{
		apiKey: apiKey,
		model:  model,
		cache:  cache,
		client: &http.Client{},
	}
}

func (g *GeminiEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if v := g.cache.Get(text); v != nil {
		return v, nil
	}

	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:embedContent?key=%s",
		g.model, g.apiKey,
	)

	body, _ := json.Marshal(map[string]any{
		"model":   "models/" + g.model,
		"content": map[string]any{"parts": []map[string]any{{"text": text}}},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embed request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini embed HTTP %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Embedding struct {
			Values []float64 `json:"values"`
		} `json:"embedding"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parsing gemini embed response: %w", err)
	}

	g.cache.Set(text, result.Embedding.Values)
	return result.Embedding.Values, nil
}

func (g *GeminiEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, t := range texts {
		v, err := g.Embed(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("batch embed [%d]: %w", i, err)
		}
		results[i] = v
	}
	return results, nil
}
