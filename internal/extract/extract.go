// Package extract fetches the full text of an article's source page, so the
// summarizer has real material instead of a short RSS teaser. Callers should
// fall back to the RSS excerpt when FetchArticle returns an error.
package extract

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

const (
	timeout   = 10 * time.Second
	userAgent = "Mozilla/5.0 (compatible; ia-personal-newsletter/1.0; +https://github.com/hbenhoud/ia-personal-newsletter)"
	// minLength guards against near-empty extractions (paywalls, JS-only
	// pages) — callers should treat these as failures and fall back.
	minLength = 200
	maxBytes  = 5 << 20 // 5MB
)

// FetchArticle downloads url and returns its extracted body text. It returns
// an error on non-200 responses, request failures, or when the extracted text
// is too short to be useful (paywalled/JS-rendered pages).
func FetchArticle(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("extract: building request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("extract: fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("extract: unexpected status %d for %s", resp.StatusCode, url)
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", fmt.Errorf("extract: parsing HTML from %s: %w", url, err)
	}

	text, err := extractText(doc)
	if err != nil {
		return "", fmt.Errorf("extract: %s: %w", url, err)
	}
	return text, nil
}

// extractText strips boilerplate and returns the article body: the <article>
// element's paragraphs when present and long enough, otherwise the densest
// cluster of sibling <p> elements on the page.
func extractText(doc *goquery.Document) (string, error) {
	doc.Find("script, style, nav, aside, header, footer, iframe, noscript, form, svg").Remove()

	if article := doc.Find("article").First(); article.Length() > 0 {
		if text := paragraphText(article); len(text) >= minLength {
			return text, nil
		}
	}

	text := densestParagraphCluster(doc.Selection)
	if len(text) < minLength {
		return "", fmt.Errorf("extracted text too short (%d chars)", len(text))
	}
	return text, nil
}

// paragraphText joins the normalized text of every <p> under sel with blank
// lines, matching how the summarizer expects paragraph breaks.
func paragraphText(sel *goquery.Selection) string {
	var paragraphs []string
	sel.Find("p").Each(func(_ int, p *goquery.Selection) {
		if t := strings.Join(strings.Fields(p.Text()), " "); t != "" {
			paragraphs = append(paragraphs, t)
		}
	})
	return strings.Join(paragraphs, "\n\n")
}

// densestParagraphCluster groups every <p> on the page by its parent element
// and returns the paragraphs of whichever parent holds the most text — a
// cheap proxy for "the actual article body" versus nav/sidebar/related links.
func densestParagraphCluster(root *goquery.Selection) string {
	totals := make(map[*html.Node]int)
	parents := make(map[*html.Node]*goquery.Selection)

	root.Find("p").Each(func(_ int, p *goquery.Selection) {
		text := strings.TrimSpace(p.Text())
		if text == "" {
			return
		}
		parent := p.Parent()
		if parent.Length() == 0 {
			return
		}
		node := parent.Get(0)
		totals[node] += len(text)
		parents[node] = parent
	})

	var best *goquery.Selection
	bestLen := -1
	for node, total := range totals {
		if total > bestLen {
			bestLen = total
			best = parents[node]
		}
	}
	if best == nil {
		return ""
	}
	return paragraphText(best)
}
