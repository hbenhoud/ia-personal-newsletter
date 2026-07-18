// Package store is the persistence layer for the dynamic product: Postgres
// (with the pgvector extension) is the source of truth for articles and
// editions. Articles are deduplicated by URL and never deleted, so archives
// persist and permalinks stay stable.
//
// Switching persistence is interface-based, mirroring the llm.Provider and
// embedding.Embedder patterns used elsewhere in the codebase.
package store

import (
	"context"
	"time"
)

// Article is one summarized item persisted in the database.
type Article struct {
	ID           int64
	URL          string
	Slug         string
	Title        string
	SourceName   string
	Author       string
	ContentHash  string
	TLDR         string
	Overview     string
	KeyPoints    []string
	WhyItMatters string
	Topic        string
	Embedding    []float32
	PublishedAt  time.Time
	FetchedAt    time.Time
	CreatedAt    time.Time
	// Rank and Score are populated only when the article is loaded as part of
	// an edition (from edition_articles).
	Rank  int
	Score float64
}

// Edition is a curated issue: an ordered set of articles for a topic.
type Edition struct {
	ID          int64
	Slug        string
	Title       string
	Topic       string
	Language    string
	PublishedAt time.Time
	// Articles is populated by GetEditionBySlug; empty in list views.
	Articles []Article
}

// EditionMember links an article to an edition with its position and score.
type EditionMember struct {
	ArticleID int64
	Rank      int
	Score     float64
}

// Store is the persistence interface. A pgx-backed implementation lives in
// postgres.go; tests may supply a fake.
type Store interface {
	// UpsertArticle inserts an article or updates the existing row matched by
	// URL, returning its id. Dedup happens here.
	UpsertArticle(ctx context.Context, a Article) (int64, error)

	// CreateEdition creates an edition and links its member articles.
	CreateEdition(ctx context.Context, e Edition, members []EditionMember) (int64, error)

	// ListEditions returns editions newest-first, optionally filtered by topic
	// (empty topic = all).
	ListEditions(ctx context.Context, topic string, limit, offset int) ([]Edition, error)

	// GetEditionBySlug returns an edition with its ordered articles.
	GetEditionBySlug(ctx context.Context, slug string) (*Edition, error)

	// GetArticleBySlug returns a single article permalink.
	GetArticleBySlug(ctx context.Context, slug string) (*Article, error)

	// ListArticles returns articles newest-first, optionally filtered by topic.
	ListArticles(ctx context.Context, topic string, limit, offset int) ([]Article, error)

	// ListTopics returns the distinct topics that have at least one edition.
	ListTopics(ctx context.Context) ([]string, error)

	// SearchArticles ranks articles by pgvector similarity to the query
	// embedding; when embedding is nil it falls back to a title/summary text
	// match on query.
	SearchArticles(ctx context.Context, query string, embedding []float32, limit int) ([]Article, error)

	// Close releases the underlying connection pool.
	Close()
}
