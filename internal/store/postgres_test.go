package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// newTestStore connects to the Postgres named by TEST_DATABASE_URL, skipping the
// test when it is unset so `go test ./...` stays green without a database.
func newTestStore(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	p, err := NewPostgres(ctx, url)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	t.Cleanup(p.Close)
	// Clean slate for a deterministic run.
	if _, err := p.pool.Exec(ctx, `TRUNCATE editions, articles RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return p
}

func TestUpsertArticleDedup(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	a := Article{
		URL: "https://example.com/a", Title: "First", SourceName: "src",
		Topic: "technical", Embedding: []float32{0.1, 0.2, 0.3},
		PublishedAt: time.Now(), FetchedAt: time.Now(),
	}
	id1, err := p.UpsertArticle(ctx, a)
	if err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	a.Title = "Updated"
	id2, err := p.UpsertArticle(ctx, a)
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("dedup failed: same URL produced ids %d and %d", id1, id2)
	}

	list, err := p.ListArticles(ctx, "technical", 10, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 article after dedup, got %d", len(list))
	}
	if list[0].Title != "Updated" {
		t.Fatalf("expected updated title, got %q", list[0].Title)
	}
}

func TestCreateAndGetEdition(t *testing.T) {
	p := newTestStore(t)
	ctx := context.Background()

	id, err := p.UpsertArticle(ctx, Article{
		URL: "https://example.com/x", Title: "X", Topic: "technical",
		PublishedAt: time.Now(), FetchedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	edID, err := p.CreateEdition(ctx,
		Edition{Slug: "technical-2026-07-18", Title: "Tech 18 Jul", Topic: "technical", Language: "en"},
		[]EditionMember{{ArticleID: id, Rank: 0, Score: 0.9}},
	)
	if err != nil {
		t.Fatalf("create edition: %v", err)
	}
	if edID == 0 {
		t.Fatal("expected non-zero edition id")
	}

	got, err := p.GetEditionBySlug(ctx, "technical-2026-07-18")
	if err != nil {
		t.Fatalf("get edition: %v", err)
	}
	if got == nil || len(got.Articles) != 1 || got.Articles[0].ID != id {
		t.Fatalf("edition articles not linked correctly: %+v", got)
	}

	topics, err := p.ListTopics(ctx)
	if err != nil {
		t.Fatalf("list topics: %v", err)
	}
	if len(topics) != 1 || topics[0] != "technical" {
		t.Fatalf("expected [technical], got %v", topics)
	}
}
