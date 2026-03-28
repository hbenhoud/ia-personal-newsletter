package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type HuggingFaceEmbedder struct {
	apiKey string
	model  string
	cache  *Cache
	client *http.Client
}

func NewHuggingFaceEmbedder(apiKey, model string, cache *Cache) *HuggingFaceEmbedder {
	return &HuggingFaceEmbedder{
		apiKey: apiKey,
		model:  model,
		cache:  cache,
		client: &http.Client{},
	}
}

func (h *HuggingFaceEmbedder) Embed(ctx context.Context, text string) ([]float64, error) {
	if v := h.cache.Get(text); v != nil {
		return v, nil
	}

	url := fmt.Sprintf("https://api-inference.huggingface.co/pipeline/feature-extraction/%s", h.model)

	body, _ := json.Marshal(map[string]any{
		"inputs":  text,
		"options": map[string]any{"wait_for_model": true},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.apiKey)

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("huggingface embed request: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("huggingface embed HTTP %d: %s", resp.StatusCode, raw)
	}

	// HF returns either []float64 (sentence-transformers) or [][]float64 (token-level)
	// Try []float64 first, then take the mean of [][]float64
	var flat []float64
	if err := json.Unmarshal(raw, &flat); err == nil {
		h.cache.Set(text, flat)
		return flat, nil
	}

	var nested [][]float64
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil, fmt.Errorf("parsing huggingface embed response: %w", err)
	}

	flat = meanPool(nested)
	h.cache.Set(text, flat)
	return flat, nil
}

func (h *HuggingFaceEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	results := make([][]float64, len(texts))
	for i, t := range texts {
		v, err := h.Embed(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("batch embed [%d]: %w", i, err)
		}
		results[i] = v
	}
	return results, nil
}

// meanPool computes the element-wise mean of token vectors.
func meanPool(vecs [][]float64) []float64 {
	if len(vecs) == 0 {
		return nil
	}
	dim := len(vecs[0])
	out := make([]float64, dim)
	for _, v := range vecs {
		for i, x := range v {
			out[i] += x
		}
	}
	n := float64(len(vecs))
	for i := range out {
		out[i] /= n
	}
	return out
}
