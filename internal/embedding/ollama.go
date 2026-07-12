package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// OllamaEmbedder talks to Ollama Cloud's native embed API (https://ollama.com/api/embed).
type OllamaEmbedder struct {
	apiKey string
	model  string
	cache  *Cache
	client *http.Client
}

func NewOllamaEmbedder(apiKey, model string, cache *Cache) *OllamaEmbedder {
	return &OllamaEmbedder{
		apiKey: apiKey,
		model:  model,
		cache:  cache,
		client: &http.Client{},
	}
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if v := o.cache.Get(text); v != nil {
		return v, nil
	}

	body, _ := json.Marshal(map[string]any{
		"model": o.model,
		"input": text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://ollama.com/api/embed",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama embed HTTP %d: %s", resp.StatusCode, raw)
	}

	// /api/embed returns {"embeddings": [[...]]} — one vector per input.
	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parsing ollama embed response: %w", err)
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed returned no vectors")
	}

	vec := result.Embeddings[0]
	o.cache.Set(text, vec)
	return vec, nil
}

func (o *OllamaEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, t := range texts {
		v, err := o.Embed(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("batch embed [%d]: %w", i, err)
		}
		results[i] = v
	}
	return results, nil
}
