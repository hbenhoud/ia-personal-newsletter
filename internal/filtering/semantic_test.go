package filtering

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
)

// --- helpers ---

func article(title, content string, daysAgo int) ingestion.Article {
	return ingestion.Article{
		Title:     title,
		URL:       "https://example.com/" + title,
		Content:   content,
		Published: time.Now().AddDate(0, 0, -daysAgo),
	}
}

func defaultCfg() Config {
	return Config{
		Mode:                "semantic",
		SimilarityThreshold: 0.5,
		RecencyDays:         7,
		MaxArticles:         10,
	}
}

// --- AC-3: pre-filter ---

func TestPreFilter_DropsOldArticles(t *testing.T) {
	articles := []ingestion.Article{
		article("recent", "content", 3),
		article("old", "content", 10),
	}
	cfg := defaultCfg()
	result := preFilter(articles, cfg)

	if len(result) != 1 {
		t.Fatalf("expected 1 article, got %d", len(result))
	}
	if result[0].Title != "recent" {
		t.Errorf("wrong article kept: %q", result[0].Title)
	}
}

func TestPreFilter_DropsExcludedKeywordInTitle(t *testing.T) {
	articles := []ingestion.Article{
		article("Bitcoin news", "blockchain stuff", 1),
		article("LLM advances", "new model released", 1),
	}
	cfg := defaultCfg()
	cfg.ExcludeKeywords = []string{"bitcoin"}
	result := preFilter(articles, cfg)

	if len(result) != 1 {
		t.Fatalf("expected 1 article, got %d", len(result))
	}
	if result[0].Title != "LLM advances" {
		t.Errorf("wrong article kept: %q", result[0].Title)
	}
}

func TestPreFilter_DropsExcludedKeywordInContent(t *testing.T) {
	articles := []ingestion.Article{
		article("Interesting title", "crypto and NFT content", 1),
		article("Good article", "about machine learning", 1),
	}
	cfg := defaultCfg()
	cfg.ExcludeKeywords = []string{"crypto"}
	result := preFilter(articles, cfg)

	if len(result) != 1 || result[0].Title != "Good article" {
		t.Errorf("exclusion on content failed: got %d articles", len(result))
	}
}

func TestPreFilter_ExclusionIsCaseInsensitive(t *testing.T) {
	articles := []ingestion.Article{
		article("CRYPTO market", "content", 1),
	}
	cfg := defaultCfg()
	cfg.ExcludeKeywords = []string{"crypto"}
	result := preFilter(articles, cfg)
	if len(result) != 0 {
		t.Error("case-insensitive exclusion failed")
	}
}

func TestPreFilter_KeepsRecentCleanArticles(t *testing.T) {
	articles := []ingestion.Article{
		article("A", "good content", 1),
		article("B", "also good", 2),
		article("C", "fine", 3),
	}
	result := preFilter(articles, defaultCfg())
	if len(result) != 3 {
		t.Errorf("expected 3 articles, got %d", len(result))
	}
}

// --- AC-3: cosine similarity ---

func TestCosineSimilarity_IdenticalVectors(t *testing.T) {
	v := []float64{1, 2, 3}
	score := cosineSimilarity(v, v)
	if math.Abs(score-1.0) > 1e-9 {
		t.Errorf("identical vectors should give 1.0, got %f", score)
	}
}

func TestCosineSimilarity_OrthogonalVectors(t *testing.T) {
	a := []float64{1, 0, 0}
	b := []float64{0, 1, 0}
	score := cosineSimilarity(a, b)
	if math.Abs(score) > 1e-9 {
		t.Errorf("orthogonal vectors should give 0.0, got %f", score)
	}
}

func TestCosineSimilarity_OppositeVectors(t *testing.T) {
	a := []float64{1, 0}
	b := []float64{-1, 0}
	score := cosineSimilarity(a, b)
	if math.Abs(score-(-1.0)) > 1e-9 {
		t.Errorf("opposite vectors should give -1.0, got %f", score)
	}
}

func TestCosineSimilarity_EmptyVectors(t *testing.T) {
	score := cosineSimilarity([]float64{}, []float64{})
	if score != 0 {
		t.Errorf("empty vectors should give 0, got %f", score)
	}
}

func TestCosineSimilarity_MismatchedLength(t *testing.T) {
	score := cosineSimilarity([]float64{1, 2}, []float64{1})
	if score != 0 {
		t.Errorf("mismatched lengths should give 0, got %f", score)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	a := []float64{0, 0, 0}
	b := []float64{1, 2, 3}
	score := cosineSimilarity(a, b)
	if score != 0 {
		t.Errorf("zero vector should give 0, got %f", score)
	}
}

// --- AC-3: Filter — ranking, threshold, max ---

// mockEmbedder returns pre-set vectors by index.
type mockEmbedder struct {
	profileVec []float64
	articleVec map[string][]float64 // keyed by article text prefix
}

func (m *mockEmbedder) Embed(_ context.Context, text string) ([]float64, error) {
	return m.profileVec, nil
}

func (m *mockEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, t := range texts {
		for prefix, vec := range m.articleVec {
			if len(t) >= len(prefix) && t[:len(prefix)] == prefix {
				result[i] = vec
				break
			}
		}
		if result[i] == nil {
			result[i] = []float64{0, 0, 1} // low similarity default
		}
	}
	return result, nil
}

func TestFilter_RankedByScoreDescending(t *testing.T) {
	// Profile vector: {1,0,0}
	// Article A: {1,0,0} → similarity 1.0
	// Article B: {0.5,0.5,0} → similarity ~0.71
	// Article C: {0,1,0} → similarity 0.0 (below threshold)
	articles := []ingestion.Article{
		article("B article", "medium", 1),
		article("A article", "high", 1),
	}

	embedder := &mockEmbedder{
		profileVec: []float64{1, 0, 0},
		articleVec: map[string][]float64{
			"B article": {0.5, 0.5, 0},
			"A article": {1, 0, 0},
		},
	}

	cfg := Config{Mode: "semantic", SimilarityThreshold: 0.5, RecencyDays: 30, MaxArticles: 10}
	scored, err := Filter(context.Background(), articles, "profile", embedder, cfg)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}

	if len(scored) < 2 {
		t.Fatalf("expected 2 scored articles, got %d", len(scored))
	}
	if scored[0].Score <= scored[1].Score {
		t.Errorf("articles not ranked descending: scores %f, %f", scored[0].Score, scored[1].Score)
	}
	if scored[0].Title != "A article" {
		t.Errorf("highest scored article should be 'A article', got %q", scored[0].Title)
	}
}

func TestFilter_ThresholdExcludes(t *testing.T) {
	articles := []ingestion.Article{
		article("Low similarity", "content", 1),
	}
	embedder := &mockEmbedder{
		profileVec: []float64{1, 0},
		articleVec: map[string][]float64{
			"Low similarity": {0, 1}, // orthogonal → similarity 0
		},
	}
	cfg := Config{Mode: "semantic", SimilarityThreshold: 0.5, RecencyDays: 30, MaxArticles: 10}
	scored, err := Filter(context.Background(), articles, "profile", embedder, cfg)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if len(scored) != 0 {
		t.Errorf("article below threshold should be excluded, got %d articles", len(scored))
	}
}

func TestFilter_MaxArticlesRespected(t *testing.T) {
	articles := make([]ingestion.Article, 5)
	for i := range articles {
		articles[i] = article("Art", "content", 1)
		articles[i].URL = "https://example.com/" + string(rune('A'+i))
	}

	embedder := &mockEmbedder{
		profileVec: []float64{1, 0},
		articleVec: map[string][]float64{
			"Art": {1, 0}, // all high similarity
		},
	}
	cfg := Config{Mode: "semantic", SimilarityThreshold: 0.0, RecencyDays: 30, MaxArticles: 3}
	scored, err := Filter(context.Background(), articles, "profile", embedder, cfg)
	if err != nil {
		t.Fatalf("Filter: %v", err)
	}
	if len(scored) > 3 {
		t.Errorf("MaxArticles=3 not respected: got %d articles", len(scored))
	}
}

func TestFilter_KeywordModeSkipsEmbedding(t *testing.T) {
	articles := []ingestion.Article{
		article("Good article", "LLM content", 1),
	}
	// embedder that always fails — should not be called in keyword mode
	embedder := &mockEmbedder{
		profileVec: nil,
		articleVec: nil,
	}
	cfg := Config{Mode: "keyword", SimilarityThreshold: 0.5, RecencyDays: 30, MaxArticles: 10}
	scored, err := Filter(context.Background(), articles, "profile", embedder, cfg)
	if err != nil {
		t.Fatalf("Filter keyword mode: %v", err)
	}
	if len(scored) != 1 {
		t.Fatalf("expected 1 article in keyword mode, got %d", len(scored))
	}
	if scored[0].Score != 1.0 {
		t.Errorf("keyword mode should assign score 1.0, got %f", scored[0].Score)
	}
}

// strictEmbedder mimics a real API (e.g. Gemini) that rejects empty text.
type strictEmbedder struct{}

func (strictEmbedder) Embed(_ context.Context, _ string) ([]float64, error) {
	return []float64{1, 0, 0}, nil
}

func (strictEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		if strings.TrimSpace(t) == "" {
			return nil, errors.New("embed HTTP 400: empty Part")
		}
		out[i] = []float64{1, 0, 0}
	}
	return out, nil
}

// An article with no title and no content must be dropped before embedding,
// otherwise the empty string fails the whole batch.
func TestFilter_SkipsEmptyText(t *testing.T) {
	articles := []ingestion.Article{
		article("", "", 1),                 // empty → must be skipped
		article("Good article", "body", 1), // valid
	}
	cfg := Config{Mode: "semantic", SimilarityThreshold: 0.5, RecencyDays: 30, MaxArticles: 10}
	scored, err := Filter(context.Background(), articles, "profile", strictEmbedder{}, cfg)
	if err != nil {
		t.Fatalf("Filter should skip empty article, got error: %v", err)
	}
	if len(scored) != 1 || scored[0].Title != "Good article" {
		t.Fatalf("expected only the valid article, got %+v", scored)
	}
}

// An empty profile must fail fast with a clear error, not an opaque API 400.
func TestFilter_EmptyProfileErrors(t *testing.T) {
	articles := []ingestion.Article{article("Good article", "body", 1)}
	cfg := Config{Mode: "semantic", SimilarityThreshold: 0.5, RecencyDays: 30, MaxArticles: 10}
	_, err := Filter(context.Background(), articles, "   \n  ", strictEmbedder{}, cfg)
	if err == nil {
		t.Fatal("expected error for empty profile text, got nil")
	}
	if !strings.Contains(err.Error(), "profile text is empty") {
		t.Errorf("error should mention empty profile, got: %v", err)
	}
}

// --- articleText truncation ---

func TestArticleText_Truncation(t *testing.T) {
	a := ingestion.Article{
		Title:   "Title",
		Content: string(make([]byte, 3000)),
	}
	text := articleText(a)
	if len(text) > 2000 {
		t.Errorf("articleText should be capped at 2000 chars, got %d", len(text))
	}
}

func TestArticleText_ShortContent(t *testing.T) {
	a := ingestion.Article{Title: "T", Content: "short"}
	text := articleText(a)
	if text != "T\nshort" {
		t.Errorf("unexpected articleText: %q", text)
	}
}
