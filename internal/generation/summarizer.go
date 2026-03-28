package generation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/hbenhoud/ia-personal-newsletter/internal/filtering"
	"github.com/hbenhoud/ia-personal-newsletter/internal/llm"
)

// Summary holds the structured output for one article.
type Summary struct {
	filtering.ScoredArticle
	TLDR          string
	WhyItMatters  string
}

type promptData struct {
	Level     string
	Interests string
	Language  string
	Title     string
	Content   string
}

// Summarizer generates summaries for a list of scored articles.
type Summarizer struct {
	provider     llm.Provider
	promptTmpl   *template.Template
	genCfg       llm.GenerationConfig
}

// NewSummarizer creates a Summarizer with the given LLM provider and prompt template string.
func NewSummarizer(provider llm.Provider, promptTemplateContent string) (*Summarizer, error) {
	tmpl, err := template.New("summarize").Parse(promptTemplateContent)
	if err != nil {
		return nil, fmt.Errorf("parsing prompt template: %w", err)
	}
	return &Summarizer{
		provider:   provider,
		promptTmpl: tmpl,
		genCfg: llm.GenerationConfig{
			MaxTokens:   300,
			Temperature: 0.3,
		},
	}, nil
}

// Summarize generates summaries for all articles sequentially.
func (s *Summarizer) Summarize(ctx context.Context, articles []filtering.ScoredArticle, level, interests, language string) ([]Summary, error) {
	summaries := make([]Summary, 0, len(articles))
	for _, a := range articles {
		sum, err := s.summarizeOne(ctx, a, level, interests, language)
		if err != nil {
			// On error, include the article with empty summary fields rather than failing the whole run
			fmt.Printf("warning: summarization failed for %q: %v\n", a.Title, err)
			summaries = append(summaries, Summary{ScoredArticle: a})
			continue
		}
		summaries = append(summaries, sum)
	}
	return summaries, nil
}

func (s *Summarizer) summarizeOne(ctx context.Context, a filtering.ScoredArticle, level, interests, language string) (Summary, error) {
	var buf bytes.Buffer
	if err := s.promptTmpl.Execute(&buf, promptData{
		Level:     level,
		Interests: interests,
		Language:  language,
		Title:     a.Title,
		Content:   truncate(a.Content, 1500),
	}); err != nil {
		return Summary{}, fmt.Errorf("rendering prompt: %w", err)
	}

	raw, err := s.provider.Complete(ctx, buf.String(), s.genCfg)
	if err != nil {
		return Summary{}, err
	}

	tldr, why := parseOutput(raw)
	return Summary{
		ScoredArticle: a,
		TLDR:          tldr,
		WhyItMatters:  why,
	}, nil
}

// parseOutput extracts TL;DR and Why it matters from the LLM response.
func parseOutput(raw string) (tldr, why string) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if t, ok := strings.CutPrefix(line, "TL;DR:"); ok {
			tldr = strings.TrimSpace(t)
		} else if w, ok := strings.CutPrefix(line, "Why it matters:"); ok {
			why = strings.TrimSpace(w)
		}
	}
	if tldr == "" {
		tldr = strings.TrimSpace(raw)
	}
	return
}

func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}
