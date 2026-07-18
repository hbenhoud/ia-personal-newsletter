package web

import (
	"encoding/json"
	"encoding/xml"
	"html/template"
	"net/http"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
)

// articleJSONLD builds schema.org NewsArticle structured data for an article.
func (s *Server) articleJSONLD(a *store.Article) template.JS {
	ld := map[string]any{
		"@context":      "https://schema.org",
		"@type":         "NewsArticle",
		"headline":      a.Title,
		"datePublished": a.PublishedAt.Format(time.RFC3339),
		"description":   a.TLDR,
		"url":           s.canonical("/articles/" + a.Slug),
		"publisher": map[string]any{
			"@type": "Organization",
			"name":  s.cfg.SiteName,
		},
	}
	if a.SourceName != "" {
		ld["author"] = map[string]any{"@type": "Organization", "name": a.SourceName}
	}
	b, err := json.Marshal(ld)
	if err != nil {
		return ""
	}
	return template.JS(b) //nolint:gosec // values are marshaled JSON, not user markup
}

// ---- RSS feed ----

type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	GUID    string `xml:"guid"`
	PubDate string `xml:"pubDate"`
	Desc    string `xml:"description"`
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	articles, err := s.store.ListArticles(r.Context(), "", 50, 0)
	if err != nil {
		s.serverError(w, "feed", err)
		return
	}
	feed := rss{
		Version: "2.0",
		Channel: rssChannel{
			Title:       s.cfg.SiteName,
			Link:        s.canonical("/"),
			Description: s.cfg.Description,
		},
	}
	for _, a := range articles {
		link := s.canonical("/articles/" + a.Slug)
		feed.Channel.Items = append(feed.Channel.Items, rssItem{
			Title:   a.Title,
			Link:    link,
			GUID:    link,
			PubDate: a.PublishedAt.Format(time.RFC1123Z),
			Desc:    a.TLDR,
		})
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(feed)
}

// ---- Sitemap ----

type urlset struct {
	XMLName xml.Name `xml:"urlset"`
	NS      string   `xml:"xmlns,attr"`
	URLs    []sitemapURL
}

type sitemapURL struct {
	XMLName xml.Name `xml:"url"`
	Loc     string   `xml:"loc"`
	LastMod string   `xml:"lastmod,omitempty"`
}

func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	add := func(path, lastmod string) {
		loc := s.canonical(path)
		if loc == "" {
			return
		}
		set.URLs = append(set.URLs, sitemapURL{Loc: loc, LastMod: lastmod})
	}

	add("/", "")
	if topics, err := s.store.ListTopics(ctx); err == nil {
		for _, t := range topics {
			add("/topics/"+t, "")
		}
	}
	if editions, err := s.store.ListEditions(ctx, "", 1000, 0); err == nil {
		for _, e := range editions {
			add("/editions/"+e.Slug, e.PublishedAt.Format("2006-01-02"))
		}
	}
	if articles, err := s.store.ListArticles(ctx, "", 5000, 0); err == nil {
		for _, a := range articles {
			add("/articles/"+a.Slug, a.PublishedAt.Format("2006-01-02"))
		}
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(set)
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	body := "User-agent: *\nAllow: /\n"
	if sm := s.canonical("/sitemap.xml"); sm != "" {
		body += "Sitemap: " + sm + "\n"
	}
	_, _ = w.Write([]byte(body))
}
