package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/filtering"
	"github.com/hbenhoud/ia-personal-newsletter/internal/generation"
	"github.com/hbenhoud/ia-personal-newsletter/internal/ingestion"
)

// minimal valid templates for testing
const (
	testNewsletterTmpl = `<!DOCTYPE html><html><head><style>{{.CSS}}</style></head><body>` +
		`{{range .Summaries}}<h2><a href="{{.URL}}">{{.Title}}</a></h2>` +
		`<p>{{.TLDR}}</p><p>{{.WhyItMatters}}</p>{{end}}` +
		`<div>{{.ArticleCount}} / {{.TotalFetched}}</div></body></html>`

	testIndexTmpl = `<!DOCTYPE html><html><head><style>{{.CSS}}</style></head><body>` +
		`{{range .Issues}}<a href="{{.Path}}">{{.Label}}</a>{{end}}</body></html>`
)

func testSummary(title, url, tldr, why string) generation.Summary {
	return generation.Summary{
		ScoredArticle: filtering.ScoredArticle{
			Article: ingestion.Article{
				Title:     title,
				URL:       url,
				Published: time.Now(),
			},
			Score: 0.9,
		},
		TLDR:         tldr,
		WhyItMatters: why,
	}
}

func newTestGenerator(t *testing.T, siteDir, css string) *Generator {
	t.Helper()
	g, err := New(siteDir, testNewsletterTmpl, testIndexTmpl, css, "minimal")
	if err != nil {
		t.Fatalf("New generator: %v", err)
	}
	return g
}

// AC-6: WriteIssue creates YYYY-WW/index.html

func TestWriteIssue_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	g := newTestGenerator(t, dir, "body{}")

	summaries := []generation.Summary{
		testSummary("LLaMA 4", "https://example.com/llama4", "New model.", "Relevant."),
	}

	outPath, err := g.WriteIssue(summaries, 10, "en", 7)
	if err != nil {
		t.Fatalf("WriteIssue: %v", err)
	}

	if _, err := os.Stat(outPath); err != nil {
		t.Errorf("output file not created: %v", err)
	}

	// Must be in a YYYY-WW subdirectory
	if !strings.Contains(outPath, "-W") {
		t.Errorf("output path should contain week dir (YYYY-WW), got %q", outPath)
	}
	if !strings.HasSuffix(outPath, "index.html") {
		t.Errorf("output path should end with index.html, got %q", outPath)
	}
}

// AC-6: CSS is embedded inline

func TestWriteIssue_CSSEmbeddedInline(t *testing.T) {
	dir := t.TempDir()
	uniqueCSS := "body { background: #unique-test-color; }"
	g := newTestGenerator(t, dir, uniqueCSS)

	outPath, err := g.WriteIssue([]generation.Summary{testSummary("T", "https://x.com", "S", "W")}, 1, "en", 7)
	if err != nil {
		t.Fatalf("WriteIssue: %v", err)
	}

	content, _ := os.ReadFile(outPath)
	if !strings.Contains(string(content), "#unique-test-color") {
		t.Error("CSS should be embedded inline in the HTML output")
	}
}

// AC-6: Article title and URL present in output

func TestWriteIssue_ArticleContentPresent(t *testing.T) {
	dir := t.TempDir()
	g := newTestGenerator(t, dir, "")

	summaries := []generation.Summary{
		testSummary("My Article Title", "https://example.com/article", "My TLDR.", "My Why."),
	}
	outPath, _ := g.WriteIssue(summaries, 5, "en", 7)
	content, _ := os.ReadFile(outPath)
	html := string(content)

	if !strings.Contains(html, "My Article Title") {
		t.Error("article title not present in output")
	}
	if !strings.Contains(html, "https://example.com/article") {
		t.Error("article URL not present in output")
	}
	if !strings.Contains(html, "My TLDR.") {
		t.Error("TL;DR not present in output")
	}
	if !strings.Contains(html, "My Why.") {
		t.Error("WhyItMatters not present in output")
	}
}

// AC-6: WriteIndex creates output/index.html

func TestWriteIndex_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	g := newTestGenerator(t, dir, "")

	// Create a fake issue directory
	issueDir := filepath.Join(dir, "2026-W13")
	if err := os.MkdirAll(issueDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(issueDir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := g.WriteIndex(); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	indexPath := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		t.Errorf("index.html not created: %v", err)
	}
}

// AC-6: WriteIndex lists past issues

func TestWriteIndex_ListsIssues(t *testing.T) {
	dir := t.TempDir()
	g := newTestGenerator(t, dir, "")

	// Create two fake issue directories
	for _, week := range []string{"2026-W10", "2026-W11"} {
		d := filepath.Join(dir, week)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(d, "index.html"), []byte("<html></html>"), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	if err := g.WriteIndex(); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	content, _ := os.ReadFile(filepath.Join(dir, "index.html"))
	html := string(content)

	if !strings.Contains(html, "2026-W10") && !strings.Contains(html, "Week 10") {
		t.Error("index should reference week 10")
	}
	if !strings.Contains(html, "2026-W11") && !strings.Contains(html, "Week 11") {
		t.Error("index should reference week 11")
	}
}

// AC-6: All 4 themes have CSS files that are non-empty

func TestThemes_AllExistAndNonEmpty(t *testing.T) {
	themes := []string{"minimal", "dark", "paper", "terminal"}
	for _, theme := range themes {
		path := filepath.Join("..", "..", "templates", "themes", theme+".css")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("theme %q: CSS file not found at %s", theme, path)
			continue
		}
		if len(data) == 0 {
			t.Errorf("theme %q: CSS file is empty", theme)
		}
		// Must declare at least one CSS rule
		if !strings.Contains(string(data), "{") {
			t.Errorf("theme %q: CSS file has no rules", theme)
		}
	}
}

// AC-6: Changing theme CSS changes the output

func TestWriteIssue_DifferentThemesProduceDifferentOutput(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	g1 := newTestGenerator(t, dir1, "body { color: red; }")
	g2 := newTestGenerator(t, dir2, "body { color: blue; }")

	summaries := []generation.Summary{testSummary("T", "https://x.com", "S", "W")}

	p1, _ := g1.WriteIssue(summaries, 1, "en", 7)
	p2, _ := g2.WriteIssue(summaries, 1, "en", 7)

	c1, _ := os.ReadFile(p1)
	c2, _ := os.ReadFile(p2)

	if string(c1) == string(c2) {
		t.Error("different theme CSS should produce different HTML output")
	}
}

// labelFromDir

func TestLabelFromDir(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"2026-W13", "Week 13, 2026"},
		{"2025-W01", "Week 01, 2025"},
	}
	for _, tt := range tests {
		got := labelFromDir(tt.dir)
		if got != tt.want {
			t.Errorf("labelFromDir(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

// AC-6: WriteIndex on non-existent site dir does not error

func TestWriteIndex_NonExistentDir(t *testing.T) {
	g := newTestGenerator(t, "/nonexistent/path/that/does/not/exist", "")
	if err := g.WriteIndex(); err != nil {
		t.Errorf("WriteIndex on non-existent dir should not error: %v", err)
	}
}
