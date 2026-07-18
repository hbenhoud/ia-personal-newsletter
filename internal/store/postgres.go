package store

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Postgres is a pgx-backed Store. Embeddings are stored in a pgvector column;
// vectors are passed/read as text and cast to `vector` in SQL, so no custom pgx
// type registration is required.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres opens a connection pool to databaseURL and applies any pending
// migrations.
func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("store: empty database URL (set DATABASE_URL)")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("store: connecting to postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: pinging postgres: %w", err)
	}
	p := &Postgres{pool: pool}
	if err := p.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

// Close releases the pool.
func (p *Postgres) Close() { p.pool.Close() }

// migrate applies embedded .sql migrations in filename order, tracking applied
// versions in a schema_migrations table.
func (p *Postgres) migrate(ctx context.Context) error {
	if _, err := p.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("store: creating schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: reading migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists bool
		if err := p.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
		).Scan(&exists); err != nil {
			return fmt.Errorf("store: checking migration %s: %w", name, err)
		}
		if exists {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: reading migration %s: %w", name, err)
		}
		if _, err := p.pool.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("store: applying migration %s: %w", name, err)
		}
		if _, err := p.pool.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name,
		); err != nil {
			return fmt.Errorf("store: recording migration %s: %w", name, err)
		}
	}
	return nil
}

// UpsertArticle inserts or updates the article matched by URL, returning its id.
func (p *Postgres) UpsertArticle(ctx context.Context, a Article) (int64, error) {
	if a.Slug == "" {
		a.Slug = Slugify(a.Title, a.URL)
	}
	if a.PublishedAt.IsZero() {
		a.PublishedAt = time.Now()
	}
	if a.FetchedAt.IsZero() {
		a.FetchedAt = time.Now()
	}
	if a.KeyPoints == nil {
		a.KeyPoints = []string{} // nil would bind as SQL NULL, violating the NOT NULL column
	}

	var id int64
	err := p.pool.QueryRow(ctx, `
		INSERT INTO articles
			(url, slug, title, source_name, author, content_hash, tldr, overview, key_points, why_matters, topic, embedding, published_at, fetched_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::vector,$13,$14)
		ON CONFLICT (url) DO UPDATE SET
			title       = EXCLUDED.title,
			source_name = EXCLUDED.source_name,
			author      = EXCLUDED.author,
			content_hash= EXCLUDED.content_hash,
			tldr        = EXCLUDED.tldr,
			overview    = EXCLUDED.overview,
			key_points  = EXCLUDED.key_points,
			why_matters = EXCLUDED.why_matters,
			topic       = EXCLUDED.topic,
			embedding   = COALESCE(EXCLUDED.embedding, articles.embedding),
			fetched_at  = EXCLUDED.fetched_at
		RETURNING id`,
		a.URL, a.Slug, a.Title, a.SourceName, a.Author, a.ContentHash,
		a.TLDR, a.Overview, a.KeyPoints, a.WhyItMatters, a.Topic, vecToText(a.Embedding),
		a.PublishedAt, a.FetchedAt,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: upserting article %q: %w", a.URL, err)
	}
	return id, nil
}

// CreateEdition creates the edition and links its members in one transaction.
func (p *Postgres) CreateEdition(ctx context.Context, e Edition, members []EditionMember) (int64, error) {
	if e.PublishedAt.IsZero() {
		e.PublishedAt = time.Now()
	}
	if e.Language == "" {
		e.Language = "en"
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO editions (slug, title, topic, language, published_at)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		e.Slug, e.Title, e.Topic, e.Language, e.PublishedAt,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: inserting edition %q: %w", e.Slug, err)
	}

	for _, m := range members {
		if _, err := tx.Exec(ctx, `
			INSERT INTO edition_articles (edition_id, article_id, rank, score)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (edition_id, article_id) DO NOTHING`,
			id, m.ArticleID, m.Rank, m.Score,
		); err != nil {
			return 0, fmt.Errorf("store: linking article %d to edition %d: %w", m.ArticleID, id, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: commit edition: %w", err)
	}
	return id, nil
}

// ListEditions returns editions newest-first, optionally filtered by topic.
func (p *Postgres) ListEditions(ctx context.Context, topic string, limit, offset int) ([]Edition, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, slug, title, topic, language, published_at
		FROM editions
		WHERE ($1 = '' OR topic = $1)
		ORDER BY published_at DESC, id DESC
		LIMIT $2 OFFSET $3`,
		topic, limitOr(limit, 50), offset,
	)
	if err != nil {
		return nil, fmt.Errorf("store: listing editions: %w", err)
	}
	defer rows.Close()

	var out []Edition
	for rows.Next() {
		var e Edition
		if err := rows.Scan(&e.ID, &e.Slug, &e.Title, &e.Topic, &e.Language, &e.PublishedAt); err != nil {
			return nil, fmt.Errorf("store: scanning edition: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEditionBySlug returns the edition with its ordered articles.
func (p *Postgres) GetEditionBySlug(ctx context.Context, slug string) (*Edition, error) {
	var e Edition
	err := p.pool.QueryRow(ctx, `
		SELECT id, slug, title, topic, language, published_at
		FROM editions WHERE slug = $1`, slug,
	).Scan(&e.ID, &e.Slug, &e.Title, &e.Topic, &e.Language, &e.PublishedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: getting edition %q: %w", slug, err)
	}

	rows, err := p.pool.Query(ctx, `
		SELECT a.id, a.url, a.slug, a.title, a.source_name, a.author,
		       a.tldr, a.why_matters, a.topic, a.published_at, a.fetched_at,
		       ea.rank, ea.score
		FROM edition_articles ea
		JOIN articles a ON a.id = ea.article_id
		WHERE ea.edition_id = $1
		ORDER BY ea.rank ASC, ea.score DESC`, e.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: getting edition articles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.URL, &a.Slug, &a.Title, &a.SourceName, &a.Author,
			&a.TLDR, &a.WhyItMatters, &a.Topic, &a.PublishedAt, &a.FetchedAt,
			&a.Rank, &a.Score); err != nil {
			return nil, fmt.Errorf("store: scanning edition article: %w", err)
		}
		e.Articles = append(e.Articles, a)
	}
	return &e, rows.Err()
}

// GetArticleBySlug returns a single article by slug (nil if not found).
func (p *Postgres) GetArticleBySlug(ctx context.Context, slug string) (*Article, error) {
	var a Article
	err := p.pool.QueryRow(ctx, `
		SELECT id, url, slug, title, source_name, author, tldr, overview, key_points, why_matters,
		       topic, published_at, fetched_at, created_at
		FROM articles WHERE slug = $1`, slug,
	).Scan(&a.ID, &a.URL, &a.Slug, &a.Title, &a.SourceName, &a.Author, &a.TLDR,
		&a.Overview, &a.KeyPoints, &a.WhyItMatters, &a.Topic, &a.PublishedAt, &a.FetchedAt, &a.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: getting article %q: %w", slug, err)
	}
	return &a, nil
}

// ListArticles returns articles newest-first, optionally filtered by topic.
func (p *Postgres) ListArticles(ctx context.Context, topic string, limit, offset int) ([]Article, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, url, slug, title, source_name, author, tldr, why_matters,
		       topic, published_at, fetched_at, created_at
		FROM articles
		WHERE ($1 = '' OR topic = $1)
		ORDER BY published_at DESC, id DESC
		LIMIT $2 OFFSET $3`,
		topic, limitOr(limit, 50), offset,
	)
	if err != nil {
		return nil, fmt.Errorf("store: listing articles: %w", err)
	}
	defer rows.Close()
	return scanArticles(rows)
}

// ListTopics returns distinct topics that have at least one edition.
func (p *Postgres) ListTopics(ctx context.Context) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT topic FROM editions WHERE topic <> '' ORDER BY topic`)
	if err != nil {
		return nil, fmt.Errorf("store: listing topics: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SearchArticles ranks by pgvector cosine distance when an embedding is given,
// otherwise falls back to an ILIKE text match on title/summary.
func (p *Postgres) SearchArticles(ctx context.Context, query string, embedding []float32, limit int) ([]Article, error) {
	limit = limitOr(limit, 20)
	if len(embedding) > 0 {
		rows, err := p.pool.Query(ctx, `
			SELECT id, url, slug, title, source_name, author, tldr, why_matters,
			       topic, published_at, fetched_at, created_at
			FROM articles
			WHERE embedding IS NOT NULL
			ORDER BY embedding <=> $1::vector
			LIMIT $2`, vecToText(embedding), limit)
		if err != nil {
			return nil, fmt.Errorf("store: semantic search: %w", err)
		}
		defer rows.Close()
		return scanArticles(rows)
	}

	pattern := "%" + strings.TrimSpace(query) + "%"
	rows, err := p.pool.Query(ctx, `
		SELECT id, url, slug, title, source_name, author, tldr, why_matters,
		       topic, published_at, fetched_at, created_at
		FROM articles
		WHERE title ILIKE $1 OR tldr ILIKE $1 OR why_matters ILIKE $1
		ORDER BY published_at DESC
		LIMIT $2`, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("store: text search: %w", err)
	}
	defer rows.Close()
	return scanArticles(rows)
}

func scanArticles(rows pgx.Rows) ([]Article, error) {
	var out []Article
	for rows.Next() {
		var a Article
		if err := rows.Scan(&a.ID, &a.URL, &a.Slug, &a.Title, &a.SourceName, &a.Author,
			&a.TLDR, &a.WhyItMatters, &a.Topic, &a.PublishedAt, &a.FetchedAt, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scanning article: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func limitOr(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

// vecToText encodes a float32 slice as pgvector's text form ("[1,2,3]"), or nil
// (→ SQL NULL) when empty.
func vecToText(v []float32) any {
	if len(v) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
