package ingestion

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// minimal valid RSS feed
func rssFeed(title, link string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <item>
      <title>%s</title>
      <link>%s</link>
      <description>Test description</description>
      <pubDate>Mon, 27 Mar 2026 10:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`, title, link)
}

// AC-2: Articles normalised into Article struct with required fields

func TestFetchFeed_NormalisesArticle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rssFeed("My Article", "https://example.com/article"))
	}))
	defer srv.Close()

	articles, err := fetchFeed(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchFeed: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}

	a := articles[0]
	if a.Title == "" {
		t.Error("Title is empty")
	}
	if a.URL == "" {
		t.Error("URL is empty")
	}
	if a.Source == "" {
		t.Error("Source is empty")
	}
	if a.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero")
	}
	if a.Published.IsZero() {
		t.Error("Published is zero")
	}
}

// AC-2: Duplicate URLs are deduplicated

func TestFetchAll_DeduplicatesByURL(t *testing.T) {
	// Two feeds serving the same URL
	sameURL := "https://example.com/same-article"

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rssFeed("Article A", sameURL))
	}))
	defer srv1.Close()

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rssFeed("Article A duplicate", sameURL))
	}))
	defer srv2.Close()

	articles, err := FetchAll(context.Background(), []string{srv1.URL, srv2.URL})
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}

	// Count how many times the same URL appears
	count := 0
	for _, a := range articles {
		if a.URL == sameURL {
			count++
		}
	}
	if count != 1 {
		t.Errorf("duplicate URL should appear only once, got %d occurrences", count)
	}
}

// AC-2: Feed failure does not crash the run

func TestFetchAll_FeedFailureContinues(t *testing.T) {
	// One good feed, one broken URL
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, rssFeed("Good Article", "https://example.com/good"))
	}))
	defer goodSrv.Close()

	feeds := []string{goodSrv.URL, "http://127.0.0.1:0/no-such-server"}

	articles, err := FetchAll(context.Background(), feeds)
	// Should not return error
	if err != nil {
		t.Fatalf("FetchAll should not fail on partial feed error: %v", err)
	}
	// Should still return articles from the working feed
	if len(articles) == 0 {
		t.Error("should have at least 1 article from the good feed")
	}
}

// AC-2: Multiple feeds fetched (concurrent)

func TestFetchAll_MultipleFeedsAllFetched(t *testing.T) {
	makeServer := func(title, link string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprint(w, rssFeed(title, link))
		}))
	}

	srv1 := makeServer("Article 1", "https://example.com/1")
	srv2 := makeServer("Article 2", "https://example.com/2")
	srv3 := makeServer("Article 3", "https://example.com/3")
	defer srv1.Close()
	defer srv2.Close()
	defer srv3.Close()

	articles, err := FetchAll(context.Background(), []string{srv1.URL, srv2.URL, srv3.URL})
	if err != nil {
		t.Fatalf("FetchAll: %v", err)
	}
	if len(articles) != 3 {
		t.Errorf("expected 3 articles from 3 feeds, got %d", len(articles))
	}
}

// AC-2: Items without a link are skipped

func TestFetchFeed_SkipsItemsWithNoLink(t *testing.T) {
	feed := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test</title>
    <item><title>No Link Item</title><description>text</description></item>
    <item><title>Has Link</title><link>https://example.com/ok</link></item>
  </channel>
</rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, feed)
	}))
	defer srv.Close()

	articles, err := fetchFeed(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("fetchFeed: %v", err)
	}
	if len(articles) != 1 {
		t.Errorf("expected 1 article (no-link item skipped), got %d", len(articles))
	}
}
