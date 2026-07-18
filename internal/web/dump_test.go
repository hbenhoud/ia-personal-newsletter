package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestDumpHTML renders key pages to DUMP_DIR for visual inspection. It only runs
// when DUMP_DIR is set, so it is a no-op in normal test runs.
func TestDumpHTML(t *testing.T) {
	dir := os.Getenv("DUMP_DIR")
	if dir == "" {
		t.Skip("DUMP_DIR not set")
	}
	s := newTestServerWithSender(t, &fakeSender{}) // email enabled → footer shows the form
	pages := map[string]string{
		"home.html":       "/",
		"edition.html":    "/editions/technical-2026-07-18",
		"article.html":    "/articles/gpt-x-launch-abc123",
		"topic.html":      "/topics/technical",
		"search.html":     "/search?q=gpt",
		"subscribed.html": "/subscribed?status=ok",
	}
	for file, path := range pages {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, req)
		if err := os.WriteFile(filepath.Join(dir, file), rec.Body.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
