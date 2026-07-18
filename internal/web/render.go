// Package web is the server-rendered (SSR) front end of the dynamic product. It
// renders pages from the store using html/template, with SEO-friendly markup.
package web

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/hbenhoud/ia-personal-newsletter/templates"
)

// PageData is the common template context; Data carries the page-specific value.
type PageData struct {
	SiteName     string
	Title        string
	Description  string
	CanonicalURL string
	OGType       string
	OGImage      string
	Language     string
	JSONLD       template.JS
	AssetVer     string
	EmailEnabled bool
	Topics       []string
	Year         int
	Data         any
}

// Renderer holds one parsed template set per page (layout + partials + page).
type Renderer struct {
	pages map[string]*template.Template
}

var pageFiles = map[string]string{
	"home":    "web/home.html",
	"edition": "web/edition.html",
	"article": "web/article.html",
	"search":  "web/search.html",
	"message": "web/message.html",
}

// NewRenderer parses the embedded web templates.
func NewRenderer() (*Renderer, error) {
	funcs := template.FuncMap{
		"title":      titleCase,
		"fmtDate":    fmtDate,
		"paragraphs": paragraphs,
	}
	pages := make(map[string]*template.Template, len(pageFiles))
	for name, file := range pageFiles {
		t, err := template.New(name).Funcs(funcs).ParseFS(
			templates.FS, "web/layout.html", "web/partials.html", file,
		)
		if err != nil {
			return nil, fmt.Errorf("web: parsing %s: %w", file, err)
		}
		pages[name] = t
	}
	return &Renderer{pages: pages}, nil
}

// Render writes the named page to w.
func (r *Renderer) Render(w io.Writer, name string, pd PageData) error {
	t, ok := r.pages[name]
	if !ok {
		return fmt.Errorf("web: unknown page %q", name)
	}
	if pd.Year == 0 {
		pd.Year = time.Now().Year()
	}
	return t.ExecuteTemplate(w, "base", pd)
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2 Jan 2006")
}

// paragraphs splits an Overview string (paragraphs separated by a blank line)
// into a slice for the template to range over as individual <p> tags.
func paragraphs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, "\n\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
