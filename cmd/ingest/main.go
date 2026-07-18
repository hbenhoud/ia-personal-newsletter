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
	"time"
	"unicode"

	"github.com/hbenhoud/ia-personal-newsletter/internal/config"
	"github.com/hbenhoud/ia-personal-newsletter/internal/dotenv"
	"github.com/hbenhoud/ia-personal-newsletter/internal/email"
	"github.com/hbenhoud/ia-personal-newsletter/internal/embedding"
	"github.com/hbenhoud/ia-personal-newsletter/internal/filtering"
	"github.com/hbenhoud/ia-personal-newsletter/internal/generation"
	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
	"github.com/hbenhoud/ia-personal-newsletter/internal/llm"
	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
	"github.com/hbenhoud/ia-personal-newsletter/templates"
)

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

	promptContent, err := templates.FS.ReadFile("prompts/summarize.tmpl")
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
	for _, pc := range cfg.Profiles {
		log.Printf("profile %q: ingesting", pc.Name)
		if err := ingestProfile(ctx, cfg, pc, embedder, llmProvider, string(promptContent), st, sender, baseURL); err != nil {
			failures++
			log.Printf("profile %q failed: %v", pc.Name, err)
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d of %d profiles failed", failures, len(cfg.Profiles))
	}
	return nil
}

// ingestProfile runs the pipeline for one profile and persists its articles and
// a new edition to the store.
func ingestProfile(
	ctx context.Context,
	cfg *config.Config,
	pc config.ProfileConfig,
	embedder embedding.Embedder,
	llmProvider llm.Provider,
	promptContent string,
	st store.Store,
	sender email.Sender,
	baseURL string,
) error {
	prof := pc.Profile

	articles, err := ingestion.FetchAll(ctx, pc.Sources.RSS)
	if err != nil {
		return fmt.Errorf("ingestion: %w", err)
	}

	scored, err := filtering.Filter(ctx, articles, prof.Text, embedder, filtering.Config{
		Mode:                cfg.Filtering.Mode,
		SimilarityThreshold: cfg.Filtering.SimilarityThreshold,
		RecencyDays:         prof.RecencyDays,
		MaxArticles:         cfg.Filtering.MaxArticles,
		ExcludeKeywords:     cfg.Filtering.ExcludeKeywords,
	})
	if err != nil {
		return fmt.Errorf("filtering: %w", err)
	}
	if len(scored) == 0 {
		log.Printf("profile %q: no articles matched; skipping edition", pc.Name)
		return nil
	}
	if cfg.Output.ItemsPerIssue > 0 && len(scored) > cfg.Output.ItemsPerIssue {
		scored = scored[:cfg.Output.ItemsPerIssue]
	}

	summarizer, err := generation.NewSummarizer(llmProvider, promptContent)
	if err != nil {
		return fmt.Errorf("creating summarizer: %w", err)
	}
	summaries, err := summarizer.Summarize(ctx, scored, prof.Level, prof.Interests, prof.Language)
	if err != nil {
		return fmt.Errorf("summarization: %w", err)
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
			WhyItMatters: sum.WhyItMatters,
			Topic:        pc.Slug(),
			Embedding:    emb,
			PublishedAt:  a.Published,
			FetchedAt:    a.FetchedAt,
		})
		if err != nil {
			return err
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
		return err
	}
	log.Printf("profile %q: edition %s with %d articles", pc.Name, editionSlug, len(members))

	// Email the new edition to subscribers (best-effort; a failure here must
	// not fail the ingest).
	if sender != nil {
		htmlBody := renderEditionEmail(baseURL, editionTitle, summaries)
		if err := sender.Broadcast(ctx, editionTitle, htmlBody); err != nil {
			log.Printf("profile %q: broadcast failed: %v", pc.Name, err)
		} else {
			log.Printf("profile %q: broadcast sent for %s", pc.Name, editionSlug)
		}
	}
	return nil
}

// renderEditionEmail builds an email-safe HTML body (inline styles) for one
// edition, linking each article to its permalink on the site.
func renderEditionEmail(baseURL, editionTitle string, summaries []generation.Summary) string {
	base := strings.TrimRight(baseURL, "/")
	var b strings.Builder
	b.WriteString(`<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;max-width:600px;margin:0 auto;padding:16px;color:#1a1a1a">`)
	b.WriteString(`<h1 style="font-size:24px;margin:0 0 24px">` + html.EscapeString(editionTitle) + `</h1>`)
	for _, sum := range summaries {
		a := sum.Article
		link := a.URL
		if base != "" {
			link = base + "/articles/" + store.Slugify(a.Title, a.URL)
		}
		b.WriteString(`<div style="margin:0 0 24px;padding-bottom:16px;border-bottom:1px solid #ececec">`)
		b.WriteString(`<div style="font-size:12px;letter-spacing:.04em;text-transform:uppercase;color:#059669;font-weight:700">` + html.EscapeString(a.Source) + `</div>`)
		b.WriteString(`<h2 style="font-size:18px;line-height:1.3;margin:6px 0"><a href="` + html.EscapeString(link) + `" style="color:#111;text-decoration:none">` + html.EscapeString(a.Title) + `</a></h2>`)
		if sum.TLDR != "" {
			b.WriteString(`<p style="font-size:15px;line-height:1.5;color:#333;margin:6px 0">` + html.EscapeString(sum.TLDR) + `</p>`)
		}
		if sum.WhyItMatters != "" {
			b.WriteString(`<p style="font-size:14px;color:#666;margin:6px 0"><strong>Why it matters:</strong> ` + html.EscapeString(sum.WhyItMatters) + `</p>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
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
