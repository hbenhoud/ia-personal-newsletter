package filtering

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/hbenhoud/ia-personal-newsletter/internal/embedding"
	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
)

// ScoredArticle is an article with its similarity score against the user profile.
type ScoredArticle struct {
	ingestion.Article
	Score float64
}

// Config controls the filtering behaviour.
type Config struct {
	Mode                string // "semantic" | "keyword" | "both"
	SimilarityThreshold float64
	RecencyDays         int
	MaxArticles         int
	ExcludeKeywords     []string
}

// Filter runs the three-pass filtering pipeline and returns ranked articles.
func Filter(
	ctx context.Context,
	articles []ingestion.Article,
	profileText string,
	embedder embedding.Embedder,
	cfg Config,
) ([]ScoredArticle, error) {
	// Pass 1: cheap pre-filter (no API calls)
	candidates := preFilter(articles, cfg)

	if cfg.Mode == "keyword" {
		// Keyword-only mode: return pre-filtered articles without scoring
		out := make([]ScoredArticle, len(candidates))
		for i, a := range candidates {
			out[i] = ScoredArticle{Article: a, Score: 1.0}
		}
		return out, nil
	}

	// Pass 2: embed profile + articles, compute cosine similarity.
	// An empty profile would make the embedding API reject the request with an
	// opaque "empty Part" error — fail fast with an actionable message instead.
	if strings.TrimSpace(profileText) == "" {
		return nil, fmt.Errorf("profile text is empty — check config/profile.md (in CI, the PROFILE_MD secret)")
	}
	profileVec, err := embedder.Embed(ctx, profileText)
	if err != nil {
		return nil, err
	}

	// Drop candidates with no embeddable text — an empty string makes the
	// embedding API reject the whole batch (e.g. Gemini "empty Part" 400).
	embedCandidates := make([]ingestion.Article, 0, len(candidates))
	texts := make([]string, 0, len(candidates))
	for _, a := range candidates {
		t := articleText(a)
		if strings.TrimSpace(t) == "" {
			continue
		}
		embedCandidates = append(embedCandidates, a)
		texts = append(texts, t)
	}

	vecs, err := embedder.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}

	scored := make([]ScoredArticle, 0, len(embedCandidates))
	for i, a := range embedCandidates {
		score := cosineSimilarity(profileVec, vecs[i])
		if score >= cfg.SimilarityThreshold {
			scored = append(scored, ScoredArticle{Article: a, Score: score})
		}
	}

	// Pass 3: rank by score descending, cap at MaxArticles
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if cfg.MaxArticles > 0 && len(scored) > cfg.MaxArticles {
		scored = scored[:cfg.MaxArticles]
	}

	return scored, nil
}

// preFilter removes articles based on recency and hard-exclusion keywords.
func preFilter(articles []ingestion.Article, cfg Config) []ingestion.Article {
	cutoff := time.Now().AddDate(0, 0, -cfg.RecencyDays)
	excludeLower := make([]string, len(cfg.ExcludeKeywords))
	for i, kw := range cfg.ExcludeKeywords {
		excludeLower[i] = strings.ToLower(kw)
	}

	var out []ingestion.Article
	for _, a := range articles {
		if a.Published.Before(cutoff) {
			continue
		}
		if containsAny(strings.ToLower(a.Title+" "+a.Content), excludeLower) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// articleText builds the text representation used for embedding.
func articleText(a ingestion.Article) string {
	text := a.Title + "\n" + a.Content
	// Cap at ~500 tokens (~2000 chars) to stay within API limits
	if len(text) > 2000 {
		// Trim at last word boundary
		text = text[:2000]
		if i := strings.LastIndexFunc(text, unicode.IsSpace); i > 0 {
			text = text[:i]
		}
	}
	return text
}

// cosineSimilarity computes dot(a,b) / (|a| * |b|).
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
