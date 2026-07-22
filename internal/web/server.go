package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/email"
	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
	"github.com/hbenhoud/ia-personal-newsletter/templates"
)

// Config holds the site identity used for SEO and rendering.
type Config struct {
	SiteName    string
	BaseURL     string // e.g. https://news.example.com (no trailing slash)
	Description string
	// GAMeasurementID enables Google Analytics 4 when set (e.g. "G-XXXXXXX").
	// The tracking script only loads after the visitor accepts the consent
	// banner (templates/web/layout.html) — never on page load itself.
	GAMeasurementID string
}

// Server serves the SSR site from the store.
type Server struct {
	store      store.Store
	renderer   *Renderer
	cfg        Config
	assetVer   string       // short hash of app.css, for cache-busting the stylesheet URL
	sender     email.Sender // nil when email is not configured
	subLimiter *rateLimiter
}

// NewServer wires a Server. sender may be nil (email not configured), in which
// case the subscribe endpoint degrades gracefully.
func NewServer(st store.Store, r *Renderer, cfg Config, sender email.Sender) *Server {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.SiteName == "" {
		cfg.SiteName = "AI Newsletter"
	}
	ver := "1"
	if css, err := templates.FS.ReadFile("web/app.css"); err == nil {
		sum := sha256.Sum256(css)
		ver = hex.EncodeToString(sum[:])[:8]
	}
	return &Server{
		store:      st,
		renderer:   r,
		cfg:        cfg,
		assetVer:   ver,
		sender:     sender,
		subLimiter: newRateLimiter(5, 10*time.Minute),
	}
}

// EmailEnabled reports whether subscriptions are configured.
func (s *Server) EmailEnabled() bool { return s.sender != nil }

// Routes returns the HTTP handler for the whole site (Go 1.22 pattern mux).
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /editions/{slug}", s.handleEdition)
	mux.HandleFunc("GET /articles/{slug}", s.handleArticle)
	mux.HandleFunc("GET /topics/{topic}", s.handleTopic)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /privacy", s.handlePrivacy)
	mux.HandleFunc("POST /api/subscribe", s.handleSubscribe)
	mux.HandleFunc("GET /subscribed", s.handleSubscribed)
	mux.HandleFunc("GET /feed.xml", s.handleFeed)
	mux.HandleFunc("GET /sitemap.xml", s.handleSitemap)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	mux.HandleFunc("GET /static/app.css", s.handleCSS)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// basePage builds a PageData pre-filled with site identity and nav topics.
func (s *Server) basePage(ctx context.Context, title, desc, path string) PageData {
	topics, err := s.store.ListTopics(ctx)
	if err != nil {
		log.Printf("web: listing topics: %v", err)
	}
	if desc == "" {
		desc = s.cfg.Description
	}
	var providerName string
	if s.sender != nil {
		providerName = titleCase(s.sender.Name())
	}
	return PageData{
		SiteName:          s.cfg.SiteName,
		Title:             title,
		Description:       desc,
		CanonicalURL:      s.canonical(path),
		Language:          "en",
		AssetVer:          s.assetVer,
		EmailEnabled:      s.sender != nil,
		EmailProviderName: providerName,
		GAMeasurementID:   s.cfg.GAMeasurementID,
		Topics:            topics,
	}
}

func (s *Server) canonical(path string) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	if path == "" || path == "/" {
		return s.cfg.BaseURL + "/"
	}
	return s.cfg.BaseURL + path
}

func (s *Server) render(w http.ResponseWriter, name string, pd PageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.renderer.Render(w, name, pd); err != nil {
		log.Printf("web: rendering %s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderMessage(w, r, http.StatusNotFound, "Page not found", "The page you're looking for doesn't exist.")
}

// homeTopic is one topic's latest edition, summarized for the landing page.
type homeTopic struct {
	Topic string
	Slug  string
	Title string
	Date  time.Time
	Lead  *store.Article
	More  []store.Article
}

// handleHome shows the latest edition per topic, grouped — never a mixed
// cross-topic feed.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	topics, err := s.store.ListTopics(ctx)
	if err != nil {
		s.serverError(w, "home topics", err)
		return
	}
	var blocks []homeTopic
	for _, t := range topics {
		latest, err := s.store.ListEditions(ctx, t, 1, 0)
		if err != nil {
			s.serverError(w, "home latest edition", err)
			return
		}
		if len(latest) == 0 {
			continue
		}
		ed, err := s.store.GetEditionBySlug(ctx, latest[0].Slug)
		if err != nil {
			s.serverError(w, "home edition articles", err)
			return
		}
		if ed == nil {
			continue
		}
		ht := homeTopic{Topic: t, Slug: ed.Slug, Title: ed.Title, Date: ed.PublishedAt}
		if len(ed.Articles) > 0 {
			ht.Lead = &ed.Articles[0]
			end := 4
			if len(ed.Articles) < end {
				end = len(ed.Articles)
			}
			ht.More = ed.Articles[1:end]
		}
		blocks = append(blocks, ht)
	}
	pd := s.basePage(ctx, s.cfg.SiteName, "", "/")
	pd.Data = struct{ Blocks []homeTopic }{blocks}
	s.render(w, "home", pd)
}

func (s *Server) handleEdition(w http.ResponseWriter, r *http.Request) {
	ed, err := s.store.GetEditionBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.serverError(w, "get edition", err)
		return
	}
	if ed == nil {
		s.notFound(w, r)
		return
	}
	s.renderEditionView(w, r, ed)
}

func (s *Server) handleArticle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	slug := r.PathValue("slug")
	a, err := s.store.GetArticleBySlug(ctx, slug)
	if err != nil {
		s.serverError(w, "get article", err)
		return
	}
	if a == nil {
		s.notFound(w, r)
		return
	}
	pd := s.basePage(ctx, a.Title, a.TLDR, "/articles/"+slug)
	pd.OGType = "article"
	pd.JSONLD = s.articleJSONLD(a)
	pd.Data = struct{ Article *store.Article }{a}
	s.render(w, "article", pd)
}

// handleTopic shows the topic's latest edition directly — never a bare list of
// dates. Older editions are reached from there via the "Past editions" selector
// and prev/next navigation.
func (s *Server) handleTopic(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	topic := r.PathValue("topic")
	latest, err := s.store.ListEditions(ctx, topic, 1, 0)
	if err != nil {
		s.serverError(w, "topic latest", err)
		return
	}
	if len(latest) == 0 {
		s.renderMessage(w, r, http.StatusOK, titleCase(topic), "Nothing published in this topic yet.")
		return
	}
	ed, err := s.store.GetEditionBySlug(ctx, latest[0].Slug)
	if err != nil {
		s.serverError(w, "topic edition", err)
		return
	}
	if ed == nil {
		s.notFound(w, r)
		return
	}
	s.renderEditionView(w, r, ed)
}

// renderEditionView renders an edition's articles with the "Past editions"
// selector and prev/next navigation. The canonical URL is always the edition's
// dated permalink, even when the view is reached via /topics/{topic}.
func (s *Server) renderEditionView(w http.ResponseWriter, r *http.Request, ed *store.Edition) {
	ctx := r.Context()

	// Prev (older) / Next (newer) within the same topic.
	var prev, next *store.Edition
	siblings, _ := s.store.ListEditions(ctx, ed.Topic, 500, 0) // newest-first
	for i := range siblings {
		if siblings[i].ID == ed.ID {
			if i > 0 {
				next = &siblings[i-1]
			}
			if i+1 < len(siblings) {
				prev = &siblings[i+1]
			}
			break
		}
	}

	desc := ed.Title
	if len(ed.Articles) > 0 && ed.Articles[0].TLDR != "" {
		desc = ed.Articles[0].TLDR
	}
	pd := s.basePage(ctx, ed.Title, desc, "/editions/"+ed.Slug)
	pd.OGType = "article"
	pd.Data = struct {
		Edition     *store.Edition
		Prev, Next  *store.Edition
		AllEditions []store.Edition
		CurrentSlug string
	}{ed, prev, next, siblings, ed.Slug}
	s.render(w, "edition", pd)
}

// renderMessage renders a simple heading + message page (empty states, 404).
func (s *Server) renderMessage(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	pd := s.basePage(r.Context(), heading, message, r.URL.Path)
	pd.Data = struct{ Heading, Message string }{heading, message}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.renderer.Render(w, "message", pd); err != nil {
		http.Error(w, message, status)
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var results []store.Article
	if q != "" {
		var err error
		// Text search (embedding=nil); semantic search can be layered in later.
		results, err = s.store.SearchArticles(ctx, q, nil, 30)
		if err != nil {
			s.serverError(w, "search", err)
			return
		}
	}
	pd := s.basePage(ctx, "Search", "Search every edition.", "/search")
	pd.Data = struct {
		Query   string
		Results []store.Article
	}{q, results}
	s.render(w, "search", pd)
}

func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	pd := s.basePage(r.Context(), "Privacy Policy", "How this site handles your data.", "/privacy")
	s.render(w, "privacy", pd)
}

// handleCSS serves the Tailwind-compiled stylesheet embedded from
// templates/web/app.css.
func (s *Server) handleCSS(w http.ResponseWriter, _ *http.Request) {
	css, err := templates.FS.ReadFile("web/app.css")
	if err != nil {
		http.Error(w, "stylesheet not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(css)
}

func (s *Server) serverError(w http.ResponseWriter, what string, err error) {
	log.Printf("web: %s: %v", what, err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}
