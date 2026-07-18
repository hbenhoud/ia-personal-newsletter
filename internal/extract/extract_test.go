package extract

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestExtractText_PrefersArticleTag(t *testing.T) {
	html := `<html><body>
		<nav><p>Home | About | Contact | Nav link filler text that should be ignored entirely here</p></nav>
		<article>
			<p>` + strings.Repeat("This is the real article body text. ", 10) + `</p>
			<p>` + strings.Repeat("More real content follows here. ", 10) + `</p>
		</article>
		<aside><p>` + strings.Repeat("Related sidebar links spam content here. ", 10) + `</p></aside>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parsing test HTML: %v", err)
	}

	got, err := extractText(doc)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	if !strings.Contains(got, "real article body") {
		t.Errorf("expected article body text, got: %q", got)
	}
	if strings.Contains(got, "sidebar") || strings.Contains(got, "Nav link") {
		t.Errorf("nav/aside content should have been stripped, got: %q", got)
	}
}

func TestExtractText_FallsBackToDensestParagraphCluster(t *testing.T) {
	// No <article> tag: pick the div with the most cumulative <p> text.
	html := `<html><body>
		<div id="sidebar"><p>Short link.</p><p>Another short link.</p></div>
		<div id="main">
			<p>` + strings.Repeat("The main content paragraph one. ", 10) + `</p>
			<p>` + strings.Repeat("The main content paragraph two. ", 10) + `</p>
			<p>` + strings.Repeat("The main content paragraph three. ", 10) + `</p>
		</div>
	</body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parsing test HTML: %v", err)
	}

	got, err := extractText(doc)
	if err != nil {
		t.Fatalf("extractText: %v", err)
	}
	if !strings.Contains(got, "main content paragraph") {
		t.Errorf("expected main content, got: %q", got)
	}
	if strings.Contains(got, "Short link") {
		t.Errorf("sidebar content should not have been picked, got: %q", got)
	}
}

func TestExtractText_ErrorsWhenTooShort(t *testing.T) {
	html := `<html><body><p>Too short.</p></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parsing test HTML: %v", err)
	}

	_, err = extractText(doc)
	if err == nil {
		t.Error("expected an error for near-empty extraction")
	}
}

func TestFetchArticle_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := FetchArticle(context.Background(), srv.URL)
	if err == nil {
		t.Error("expected an error for a 404 response")
	}
}

func TestFetchArticle_Success(t *testing.T) {
	body := `<html><body><article><p>` + strings.Repeat("Real fetched article content. ", 15) + `</p></article></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Error("expected a User-Agent header to be set")
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := FetchArticle(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchArticle: %v", err)
	}
	if !strings.Contains(got, "Real fetched article content") {
		t.Errorf("expected fetched content, got: %q", got)
	}
}
