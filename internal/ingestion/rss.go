package ingestion

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mmcdole/gofeed"
)

// Article is a normalised feed item.
type Article struct {
	Title     string
	URL       string
	Content   string // description or full content
	Source    string // feed URL
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
			Source:    url,
			FetchedAt: time.Now(),
			Published: published,
		})
	}

	return articles, nil
}
