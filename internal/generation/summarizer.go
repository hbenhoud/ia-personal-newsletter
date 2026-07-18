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

// Summary holds the structured output for one article. Overview and KeyPoints
// make the article page self-sufficient; TLDR/WhyItMatters stay short for
// cards and feeds.
type Summary struct {
	filtering.ScoredArticle
	TLDR         string
	Overview     string
	KeyPoints    []string
	WhyItMatters string
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
	provider        llm.Provider
	promptTmpl      *template.Template
	genCfg          llm.GenerationConfig
	contentMaxChars int
	OnProgress      func(done, total int) // optional; called after each article
}

// Options customizes a Summarizer beyond NewSummarizer's defaults. Zero values
// fall back to the defaults, so callers only set what they need to change —
// e.g. the dynamic product raises both to fit a full-article, structured
// summary instead of the frozen static CLI's two-line one.
type Options struct {
	MaxTokens       int // 0 = defaultMaxTokens
	ContentMaxChars int // 0 = defaultContentMaxChars
}

const (
	defaultMaxTokens       = 300
	defaultContentMaxChars = 1500
)

// NewSummarizer creates a Summarizer with the given LLM provider and prompt
// template string, using the default generation limits.
func NewSummarizer(provider llm.Provider, promptTemplateContent string) (*Summarizer, error) {
	return NewSummarizerWithOptions(provider, promptTemplateContent, Options{})
}

// NewSummarizerWithOptions is like NewSummarizer but lets the caller override
// MaxTokens/ContentMaxChars for a richer prompt.
func NewSummarizerWithOptions(provider llm.Provider, promptTemplateContent string, opts Options) (*Summarizer, error) {
	tmpl, err := template.New("summarize").Parse(promptTemplateContent)
	if err != nil {
		return nil, fmt.Errorf("parsing prompt template: %w", err)
	}
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = defaultMaxTokens
	}
	contentMaxChars := opts.ContentMaxChars
	if contentMaxChars == 0 {
		contentMaxChars = defaultContentMaxChars
	}
	return &Summarizer{
		provider:   provider,
		promptTmpl: tmpl,
		genCfg: llm.GenerationConfig{
			MaxTokens:   maxTokens,
			Temperature: 0.3,
		},
		contentMaxChars: contentMaxChars,
	}, nil
}

// Summarize generates summaries for all articles sequentially.
func (s *Summarizer) Summarize(ctx context.Context, articles []filtering.ScoredArticle, level, interests, language string) ([]Summary, error) {
	summaries := make([]Summary, 0, len(articles))
	for i, a := range articles {
		sum, err := s.summarizeOne(ctx, a, level, interests, language)
		if err != nil {
			// On error, include the article with empty summary fields rather than failing the whole run
			fmt.Printf("warning: summarization failed for %q: %v\n", a.Title, err)
			summaries = append(summaries, Summary{ScoredArticle: a})
		} else {
			summaries = append(summaries, sum)
		}
		if s.OnProgress != nil {
			s.OnProgress(i+1, len(articles))
		}
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
		Content:   truncate(a.Content, s.contentMaxChars),
	}); err != nil {
		return Summary{}, fmt.Errorf("rendering prompt: %w", err)
	}

	raw, err := s.provider.Complete(ctx, buf.String(), s.genCfg)
	if err != nil {
		return Summary{}, err
	}

	tldr, overview, keyPoints, why := parseOutput(raw)
	return Summary{
		ScoredArticle: a,
		TLDR:          tldr,
		Overview:      overview,
		KeyPoints:     keyPoints,
		WhyItMatters:  why,
	}, nil
}

// section names tracked while scanning the LLM response line by line.
const (
	sectionNone = iota
	sectionOverview
	sectionKeyPoints
)

// parseOutput extracts the four labeled sections from the LLM response:
// TL;DR and Why it matters are single lines; Overview collects one paragraph
// per non-blank line until the next label; Key points collects "- " bullets.
// Falls back to using the raw response as the TL;DR when no labels are found.
func parseOutput(raw string) (tldr, overview string, keyPoints []string, why string) {
	section := sectionNone
	var overviewParagraphs []string

	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "TL;DR:"):
			tldr = strings.TrimSpace(strings.TrimPrefix(trimmed, "TL;DR:"))
			section = sectionNone
			continue
		case strings.HasPrefix(trimmed, "Overview:"):
			section = sectionOverview
			if rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "Overview:")); rest != "" {
				overviewParagraphs = append(overviewParagraphs, rest)
			}
			continue
		case strings.HasPrefix(trimmed, "Key points:"):
			section = sectionKeyPoints
			continue
		case strings.HasPrefix(trimmed, "Why it matters:"):
			why = strings.TrimSpace(strings.TrimPrefix(trimmed, "Why it matters:"))
			section = sectionNone
			continue
		}

		if trimmed == "" {
			continue
		}
		switch section {
		case sectionOverview:
			overviewParagraphs = append(overviewParagraphs, trimmed)
		case sectionKeyPoints:
			keyPoints = append(keyPoints, trimBullet(trimmed))
		}
	}

	overview = strings.Join(overviewParagraphs, "\n\n")
	if tldr == "" {
		tldr = strings.TrimSpace(raw)
	}
	return
}

// trimBullet strips a leading "- ", "* " or "• " marker from a key point line.
func trimBullet(s string) string {
	for _, prefix := range []string{"- ", "* ", "• "} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			return strings.TrimSpace(rest)
		}
	}
	return s
}

func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars]
}
