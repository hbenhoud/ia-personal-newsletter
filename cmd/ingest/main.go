// Command ingest runs the newsletter pipeline (RSS → filter → summarize) and
// persists the results to Postgres, which is the source of truth for the
// dynamic product. It is the DB-backed counterpart of the frozen static
// `newsletter generate`, meant to run on a schedule (CI cron or a worker).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/hbenhoud/ia-personal-newsletter/internal/config"
	"github.com/hbenhoud/ia-personal-newsletter/internal/dotenv"
	"github.com/hbenhoud/ia-personal-newsletter/internal/email"
	"github.com/hbenhoud/ia-personal-newsletter/internal/embedding"
	"github.com/hbenhoud/ia-personal-newsletter/internal/extract"
	"github.com/hbenhoud/ia-personal-newsletter/internal/filtering"
	"github.com/hbenhoud/ia-personal-newsletter/internal/generation"
	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
	"github.com/hbenhoud/ia-personal-newsletter/internal/llm"
	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
	"github.com/hbenhoud/ia-personal-newsletter/templates"
)

// maxConcurrentFetches bounds how many full-article page fetches run at once
// during enrichment, so a slow/misbehaving source can't stall the whole run.
const maxConcurrentFetches = 4

const configDir = "./config"

func main() {
	dotenv.Load(".env")
	if err := run(context.Background()); err != nil {
		log.Fatalf("ingest: %v", err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}

	databaseURL := os.Getenv("DATABASE_URL")
	st, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	// Shared providers, reused across profiles (the embedding cache is
	// content-hashed, so reuse is safe).
	embedder, cache, err := embedding.NewEmbedder(
		cfg.Embedding.Provider, cfg.Embedding.Model,
		cfg.Embedding.APIKeyEnv, cfg.Embedding.CachePath,
	)
	if err != nil {
		return fmt.Errorf("embedding provider: %w", err)
	}
	defer func() {
		if flushErr := cache.Flush(); flushErr != nil {
			log.Printf("warning: flushing embedding cache: %v", flushErr)
		}
	}()

	llmProvider, err := llm.NewProvider(cfg.LLM.Provider, cfg.LLM.Model, cfg.LLM.APIKeyEnv)
	if err != nil {
		return fmt.Errorf("LLM provider: %w", err)
	}

	// The dynamic product uses its own richer prompt (full overview + key
	// points) so the frozen static CLI's prompt/cost profile stays untouched.
	promptContent, err := templates.FS.ReadFile("prompts/summarize_rich.tmpl")
	if err != nil {
		return fmt.Errorf("reading embedded prompt template: %w", err)
	}

	// Email broadcast is optional: without EMAIL_API_KEY, editions are still
	// persisted, just not emailed.
	var sender email.Sender
	if ecfg, ok := email.ConfigFromEnv(os.Getenv); ok {
		sender, err = email.NewSender(ecfg)
		if err != nil {
			return fmt.Errorf("email provider: %w", err)
		}
		log.Printf("ingest: broadcast enabled (%s)", sender.Name())
	}
	baseURL := os.Getenv("SITE_BASE_URL")

	var failures int
	var sections []editionSection
	for _, pc := range cfg.Profiles {
		log.Printf("profile %q: ingesting", pc.Name)
		data, err := ingestProfile(ctx, cfg, pc, embedder, llmProvider, string(promptContent), st)
		if err != nil {
			failures++
			log.Printf("profile %q failed: %v", pc.Name, err)
			continue
		}
		if data != nil {
			sections = append(sections, *data)
		}
	}

	// One combined email per ingest run, covering every profile's new edition —
	// not one email per profile, so subscribers aren't spammed per topic. A
	// single global audience for now; per-profile targeting is deferred until
	// the profile list stabilizes.
	if sender != nil && len(sections) > 0 {
		subject := fmt.Sprintf("%s · %s", envOr("SITE_NAME", "AI Newsletter"), time.Now().Format("2 Jan 2006"))
		htmlBody := renderCombinedEmail(baseURL, subject, sections)
		if err := sender.Broadcast(ctx, subject, htmlBody); err != nil {
			log.Printf("ingest: broadcast failed: %v", err)
		} else {
			log.Printf("ingest: broadcast sent (%d section(s))", len(sections))
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d of %d profiles failed", failures, len(cfg.Profiles))
	}
	return nil
}

// editionSection is one profile's contribution to the combined email.
type editionSection struct {
	Title     string
	Slug      string
	Summaries []generation.Summary
}

// ingestProfile runs the pipeline for one profile and persists its articles and
// a new edition to the store. It returns the data needed for that profile's
// section of the combined email, or nil if no edition was created.
func ingestProfile(
	ctx context.Context,
	cfg *config.Config,
	pc config.ProfileConfig,
	embedder embedding.Embedder,
	llmProvider llm.Provider,
	promptContent string,
	st store.Store,
) (*editionSection, error) {
	prof := pc.Profile

	articles, err := ingestion.FetchAll(ctx, pc.Sources.RSS)
	if err != nil {
		return nil, fmt.Errorf("ingestion: %w", err)
	}

	scored, err := filtering.Filter(ctx, articles, prof.Text, embedder, filtering.Config{
		Mode:                cfg.Filtering.Mode,
		SimilarityThreshold: cfg.Filtering.SimilarityThreshold,
		RecencyDays:         prof.RecencyDays,
		MaxArticles:         cfg.Filtering.MaxArticles,
		ExcludeKeywords:     cfg.Filtering.ExcludeKeywords,
	})
	if err != nil {
		return nil, fmt.Errorf("filtering: %w", err)
	}
	if len(scored) == 0 {
		log.Printf("profile %q: no articles matched; skipping edition", pc.Name)
		return nil, nil
	}
	if cfg.Output.ItemsPerIssue > 0 && len(scored) > cfg.Output.ItemsPerIssue {
		scored = scored[:cfg.Output.ItemsPerIssue]
	}

	// Fetch each selected article's full text so the summary can be
	// self-sufficient; on failure (paywall, timeout, blocked) keep the RSS
	// excerpt already in scored[i].Article.Content.
	enrichWithFullText(ctx, pc.Name, scored)

	summarizer, err := generation.NewSummarizerWithOptions(llmProvider, promptContent, generation.Options{
		MaxTokens:       900,
		ContentMaxChars: 6000,
	})
	if err != nil {
		return nil, fmt.Errorf("creating summarizer: %w", err)
	}
	summaries, err := summarizer.Summarize(ctx, scored, prof.Level, prof.Interests, prof.Language)
	if err != nil {
		return nil, fmt.Errorf("summarization: %w", err)
	}

	// Persist each article (dedup on URL) with its embedding, then create the
	// edition linking them in ranked order.
	members := make([]store.EditionMember, 0, len(summaries))
	for i, sum := range summaries {
		a := sum.Article

		// Reuse the cached embedding vector for this article's text.
		var emb []float32
		if vec, embErr := embedder.Embed(ctx, filtering.ArticleText(a)); embErr == nil {
			emb = toFloat32(vec)
		} else {
			log.Printf("profile %q: embedding %q failed: %v", pc.Name, a.Title, embErr)
		}

		id, err := st.UpsertArticle(ctx, store.Article{
			URL:          a.URL,
			Title:        a.Title,
			SourceName:   a.Source,
			ContentHash:  contentHash(a.Title, a.Content),
			TLDR:         sum.TLDR,
			Overview:     sum.Overview,
			KeyPoints:    sum.KeyPoints,
			WhyItMatters: sum.WhyItMatters,
			Topic:        pc.Slug(),
			Embedding:    emb,
			PublishedAt:  a.Published,
			FetchedAt:    a.FetchedAt,
		})
		if err != nil {
			return nil, err
		}
		members = append(members, store.EditionMember{ArticleID: id, Rank: i, Score: sum.Score})
	}

	now := time.Now()
	editionSlug := fmt.Sprintf("%s-%s", pc.Slug(), now.Format("2006-01-02"))
	editionTitle := fmt.Sprintf("%s · %s", title(pc.Name), now.Format("2 Jan 2006"))
	if _, err := st.CreateEdition(ctx, store.Edition{
		Slug:        editionSlug,
		Title:       editionTitle,
		Topic:       pc.Slug(),
		Language:    prof.Language,
		PublishedAt: now,
	}, members); err != nil {
		return nil, err
	}
	log.Printf("profile %q: edition %s with %d articles", pc.Name, editionSlug, len(members))

	return &editionSection{Title: editionTitle, Slug: editionSlug, Summaries: summaries}, nil
}

// enrichWithFullText fetches each article's source page and, on success,
// replaces its RSS excerpt with the full extracted text — bounded to
// maxConcurrentFetches at a time so one slow source can't stall the run.
// Failures (paywall, timeout, blocked) are logged and leave the excerpt as-is.
func enrichWithFullText(ctx context.Context, profileName string, scored []filtering.ScoredArticle) {
	sem := make(chan struct{}, maxConcurrentFetches)
	var wg sync.WaitGroup
	for i := range scored {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			full, err := extract.FetchArticle(ctx, scored[i].Article.URL)
			if err != nil {
				log.Printf("profile %q: full-text fetch failed for %q, using RSS excerpt: %v",
					profileName, scored[i].Article.Title, err)
				return
			}
			scored[i].Article.Content = full
		}(i)
	}
	wg.Wait()
}

// maxArticlesPerEmailSection caps how many articles from one profile's
// edition appear in the combined email — editions can hold up to
// ItemsPerIssue (20) articles, too many to read in one email. Summaries
// arrive pre-ranked by score, so this keeps each section's best picks and
// links to the full edition on the site for the rest.
const maxArticlesPerEmailSection = 5

// renderCombinedEmail builds an email-safe HTML body (inline styles) covering
// every profile's new edition from one ingest run, so subscribers get a single
// email per run rather than one per profile. Each section shows its top
// articles (linked to their permalink) plus a link to the full edition.
func renderCombinedEmail(baseURL, subject string, sections []editionSection) string {
	base := strings.TrimRight(baseURL, "/")
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;max-width:600px;margin:0 auto;padding:16px;color:#1a1a1a">`)
	b.WriteString(`<h1 style="font-size:24px;margin:0 0 24px">` + html.EscapeString(subject) + `</h1>`)
	for _, sec := range sections {
		b.WriteString(`<h2 style="font-size:13px;letter-spacing:.04em;text-transform:uppercase;color:#059669;font-weight:700;margin:28px 0 12px">` + html.EscapeString(sec.Title) + `</h2>`)
		top := sec.Summaries
		if len(top) > maxArticlesPerEmailSection {
			top = top[:maxArticlesPerEmailSection]
		}
		for _, sum := range top {
			a := sum.Article
			link := a.URL
			if base != "" {
				link = base + "/articles/" + store.Slugify(a.Title, a.URL)
			}
			b.WriteString(`<div style="margin:0 0 24px;padding-bottom:16px;border-bottom:1px solid #ececec">`)
			b.WriteString(`<div style="font-size:12px;color:#888">` + html.EscapeString(a.Source) + `</div>`)
			b.WriteString(`<h3 style="font-size:18px;line-height:1.3;margin:6px 0"><a href="` + html.EscapeString(link) + `" style="color:#111;text-decoration:none">` + html.EscapeString(a.Title) + `</a></h3>`)
			if sum.TLDR != "" {
				b.WriteString(`<p style="font-size:15px;line-height:1.5;color:#333;margin:6px 0">` + html.EscapeString(sum.TLDR) + `</p>`)
			}
			b.WriteString(`</div>`)
		}
		if base != "" && len(sec.Summaries) > maxArticlesPerEmailSection {
			b.WriteString(`<p style="margin:0 0 8px"><a href="` + html.EscapeString(base+"/editions/"+sec.Slug) + `" style="color:#059669;font-size:14px;font-weight:600;text-decoration:none">See all ` + fmt.Sprint(len(sec.Summaries)) + ` articles →</a></p>`)
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func toFloat32(v []float64) []float32 {
	out := make([]float32, len(v))
	for i, f := range v {
		out[i] = float32(f)
	}
	return out
}

func contentHash(title, content string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + content))
	return hex.EncodeToString(sum[:])
}

// title upper-cases the first rune, e.g. "technical" → "Technical".
func title(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}
