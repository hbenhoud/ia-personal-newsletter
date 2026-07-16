package ingestion

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

// Article is a normalised feed item.
type Article struct {
	Title     string
	URL       string
	Content   string // description or full content
	Source    string // human-readable feed name (falls back to the feed host)
	FetchedAt time.Time
	Published time.Time
}

// FetchAll concurrently fetches all RSS feeds and returns deduplicated articles.
func FetchAll(ctx context.Context, feeds []string) ([]Article, error) {
	type result struct {
		articles []Article
		err      error
		feed     string
	}

	ch := make(chan result, len(feeds))
	for _, feed := range feeds {
		go func(url string) {
			articles, err := fetchFeed(ctx, url)
			ch <- result{articles: articles, err: err, feed: url}
		}(feed)
	}

	seen := make(map[string]struct{})
	var all []Article

	for range feeds {
		r := <-ch
		if r.err != nil {
			log.Printf("warning: failed to fetch feed %s: %v", r.feed, r.err)
			continue
		}
		for _, a := range r.articles {
			if _, dup := seen[a.URL]; dup {
				continue
			}
			seen[a.URL] = struct{}{}
			all = append(all, a)
		}
	}

	return all, nil
}

func fetchFeed(ctx context.Context, url string) ([]Article, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(url, ctx)
	if err != nil {
		return nil, fmt.Errorf("parsing feed: %w", err)
	}

	source := strings.TrimSpace(feed.Title)
	if source == "" {
		source = sourceHost(url)
	}

	articles := make([]Article, 0, len(feed.Items))
	for _, item := range feed.Items {
		if item.Link == "" {
			continue
		}

		content := item.Description
		if content == "" {
			content = item.Content
		}

		published := time.Now()
		if item.PublishedParsed != nil {
			published = *item.PublishedParsed
		} else if item.UpdatedParsed != nil {
			published = *item.UpdatedParsed
		}

		articles = append(articles, Article{
			Title:     item.Title,
			URL:       item.Link,
			Content:   content,
			Source:    source,
			FetchedAt: time.Now(),
			Published: published,
		})
	}

	return articles, nil
}

// sourceHost returns the feed's host without a leading "www.", used as a
// fallback source label when the feed has no title.
func sourceHost(feedURL string) string {
	if u, err := url.Parse(feedURL); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Host, "www.")
	}
	return feedURL
}
