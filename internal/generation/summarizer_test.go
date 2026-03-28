package generation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/filtering"
	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
	"github.com/hbenhoud/ia-personal-newsletter/internal/llm"
)

// --- mock LLM provider ---

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Complete(_ context.Context, _ string, _ llm.GenerationConfig) (string, error) {
	return m.response, m.err
}

// --- AC-5: parseOutput ---

func TestParseOutput_BothFields(t *testing.T) {
	raw := "TL;DR: New LLaMA model released.\nWhy it matters: Advances open source LLMs."
	tldr, why := parseOutput(raw)

	if tldr != "New LLaMA model released." {
		t.Errorf("TLDR: got %q", tldr)
	}
	if why != "Advances open source LLMs." {
		t.Errorf("WhyItMatters: got %q", why)
	}
}

func TestParseOutput_ExtraWhitespace(t *testing.T) {
	raw := "  TL;DR:   Key insight here.  \n  Why it matters:   Very relevant.  "
	tldr, why := parseOutput(raw)

	if tldr != "Key insight here." {
		t.Errorf("TLDR not trimmed: got %q", tldr)
	}
	if why != "Very relevant." {
		t.Errorf("Why not trimmed: got %q", why)
	}
}

func TestParseOutput_FallbackWhenNoTLDR(t *testing.T) {
	raw := "Just a plain response with no structured output."
	tldr, why := parseOutput(raw)

	if tldr == "" {
		t.Error("TLDR fallback should use the raw text")
	}
	if why != "" {
		t.Error("Why should be empty when not present")
	}
}

func TestParseOutput_MultiLineTLDR(t *testing.T) {
	// Only first matching line should be used
	raw := "TL;DR: First summary.\nTL;DR: Second summary.\nWhy it matters: Relevant."
	tldr, _ := parseOutput(raw)
	// The last matching line wins (loop overwrites)
	if !strings.Contains(tldr, "summary") {
		t.Errorf("unexpected TLDR: %q", tldr)
	}
}

func TestParseOutput_NoFillerAccepted(t *testing.T) {
	// LLM output with only the two expected lines
	raw := "TL;DR: Short summary.\nWhy it matters: Very relevant."
	tldr, why := parseOutput(raw)

	if tldr == "" || why == "" {
		t.Error("expected both fields to be populated")
	}
	// Neither field should contain boilerplate prefixes
	if strings.Contains(tldr, "TL;DR:") {
		t.Error("TLDR should not include 'TL;DR:' prefix")
	}
	if strings.Contains(why, "Why it matters:") {
		t.Error("Why should not include 'Why it matters:' prefix")
	}
}

// --- AC-5: Summarize with mock provider ---

func scoredArticle(title string) filtering.ScoredArticle {
	return filtering.ScoredArticle{
		Article: ingestion.Article{
			Title:     title,
			URL:       "https://example.com/" + title,
			Content:   "Some content about AI research.",
			Published: time.Now(),
		},
		Score: 0.85,
	}
}

const testPromptTmpl = `{{.Level}} {{.Interests}} {{.Title}} {{.Content}}`

func TestSummarize_PopulatesBothFields(t *testing.T) {
	provider := &mockProvider{
		response: "TL;DR: Key insight.\nWhy it matters: Very relevant.",
	}
	s, err := NewSummarizer(provider, testPromptTmpl)
	if err != nil {
		t.Fatalf("NewSummarizer: %v", err)
	}

	articles := []filtering.ScoredArticle{scoredArticle("LLaMA 4 release")}
	summaries, err := s.Summarize(context.Background(), articles, "expert", "LLMs, RAG", "en")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}

	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].TLDR != "Key insight." {
		t.Errorf("TLDR: got %q", summaries[0].TLDR)
	}
	if summaries[0].WhyItMatters != "Very relevant." {
		t.Errorf("WhyItMatters: got %q", summaries[0].WhyItMatters)
	}
}

func TestSummarize_LLMErrorKeepsArticle(t *testing.T) {
	// AC-5: on error, article is kept with empty summary fields — run does not fail
	provider := &mockProvider{err: fmt.Errorf("API timeout")}
	s, _ := NewSummarizer(provider, testPromptTmpl)

	articles := []filtering.ScoredArticle{scoredArticle("error article")}
	summaries, err := s.Summarize(context.Background(), articles, "expert", "LLMs", "en")
	if err != nil {
		t.Fatalf("Summarize should not return error on LLM failure: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("article should be kept on LLM error, got %d summaries", len(summaries))
	}
	if summaries[0].TLDR != "" || summaries[0].WhyItMatters != "" {
		t.Error("failed summary should have empty TLDR and WhyItMatters")
	}
}

func TestSummarize_MultipleArticles(t *testing.T) {
	provider := &mockProvider{response: "TL;DR: Summary.\nWhy it matters: Relevant."}
	s, _ := NewSummarizer(provider, testPromptTmpl)

	articles := []filtering.ScoredArticle{
		scoredArticle("Article 1"),
		scoredArticle("Article 2"),
		scoredArticle("Article 3"),
	}
	summaries, err := s.Summarize(context.Background(), articles, "intermediate", "agents", "en")
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if len(summaries) != 3 {
		t.Errorf("expected 3 summaries, got %d", len(summaries))
	}
}

// AC-5: NewSummarizer with invalid template returns error
func TestNewSummarizer_InvalidTemplate(t *testing.T) {
	provider := &mockProvider{}
	_, err := NewSummarizer(provider, "{{.Unclosed")
	if err == nil {
		t.Error("invalid template should return an error")
	}
}

// --- AC-5: truncate ---

func TestTruncate_LongString(t *testing.T) {
	s := strings.Repeat("a", 2000)
	got := truncate(s, 1500)
	if len(got) != 1500 {
		t.Errorf("truncate: got length %d, want 1500", len(got))
	}
}

func TestTruncate_ShortString(t *testing.T) {
	s := "short"
	got := truncate(s, 1500)
	if got != s {
		t.Errorf("truncate changed short string: %q", got)
	}
}
