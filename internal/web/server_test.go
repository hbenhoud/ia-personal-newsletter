package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hbenhoud/ia-personal-newsletter/internal/store"
)

// fakeStore is an in-memory store.Store for exercising the HTTP layer without
// Postgres.
type fakeStore struct {
	articles []store.Article
	editions []store.Edition
}

func (f *fakeStore) UpsertArticle(context.Context, store.Article) (int64, error) { return 0, nil }
func (f *fakeStore) CreateEdition(context.Context, store.Edition, []store.EditionMember) (int64, error) {
	return 0, nil
}
func (f *fakeStore) ListEditions(_ context.Context, topic string, limit, offset int) ([]store.Edition, error) {
	var out []store.Edition
	for _, e := range f.editions {
		if topic == "" || e.Topic == topic {
			out = append(out, e)
		}
	}
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeStore) GetEditionBySlug(_ context.Context, slug string) (*store.Edition, error) {
	for i := range f.editions {
		if f.editions[i].Slug == slug {
			e := f.editions[i]
			for _, a := range f.articles { // only this topic's articles
				if a.Topic == e.Topic {
					e.Articles = append(e.Articles, a)
				}
			}
			return &e, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) GetArticleBySlug(_ context.Context, slug string) (*store.Article, error) {
	for i := range f.articles {
		if f.articles[i].Slug == slug {
			return &f.articles[i], nil
		}
	}
	return nil, nil
}
func (f *fakeStore) ListArticles(_ context.Context, topic string, _, _ int) ([]store.Article, error) {
	var out []store.Article
	for _, a := range f.articles {
		if topic == "" || a.Topic == topic {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeStore) ListTopics(context.Context) ([]string, error) {
	return []string{"technical", "business"}, nil
}
func (f *fakeStore) SearchArticles(_ context.Context, q string, _ []float32, _ int) ([]store.Article, error) {
	var out []store.Article
	for _, a := range f.articles {
		if strings.Contains(strings.ToLower(a.Title), strings.ToLower(q)) {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *fakeStore) Close() {}

// makeEditions builds n editions for a topic, newest first, dated back from
// 18 Jul 2026 (so the first technical slug is "technical-2026-07-18").
func makeEditions(topic string, n int) []store.Edition {
	var eds []store.Edition
	for i := 0; i < n; i++ {
		d := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -i)
		eds = append(eds, store.Edition{
			ID:          int64(i + 1),
			Slug:        topic + "-" + d.Format("2006-01-02"),
			Title:       titleCase(topic) + " · " + d.Format("2 Jan 2006"),
			Topic:       topic,
			Language:    "en",
			PublishedAt: d,
		})
	}
	return eds
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	r, err := NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	fs := &fakeStore{
		articles: []store.Article{
			{
				ID: 1, Slug: "gpt-x-launch-abc123", URL: "https://ex.com/1",
				Title: "GPT-X launch reshapes the frontier", SourceName: "Simon Willison", Topic: "technical",
				TLDR: "A new frontier model shipped with a far larger context window and lower latency.",
				WhyItMatters: "It resets the price/performance baseline every builder plans around.",
				Score:        0.94, Rank: 0,
				PublishedAt: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
			},
			{
				ID: 2, Slug: "open-weights-catch-up-def456", URL: "https://ex.com/2",
				Title: "Open-weight models close the gap", SourceName: "Hugging Face", Topic: "technical",
				TLDR: "The latest open releases now trail the best closed models by a single-digit margin.",
				WhyItMatters: "Self-hosting becomes viable for teams that ruled it out a year ago.",
				Score:        0.81, Rank: 1,
				PublishedAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
			},
			{
				ID: 3, Slug: "agents-in-prod-ghi789", URL: "https://ex.com/3",
				Title: "Agents quietly move into production", SourceName: "LangChain", Topic: "technical",
				TLDR: "Teams report durable, tool-using agents running real workflows at scale.",
				WhyItMatters: "The demo era is ending; reliability and evals are the new battleground.",
				Score:        0.76, Rank: 2,
				PublishedAt: time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC),
			},
			{
				ID: 4, Slug: "enterprise-ai-budgets-jkl012", URL: "https://ex.com/4",
				Title: "Enterprise AI budgets shift to inference", SourceName: "The Information", Topic: "business",
				TLDR: "CIOs are reallocating spend from training pilots to production inference.",
				WhyItMatters: "Vendors that price on tokens win; those pricing on seats lose.",
				Score:        0.88, Rank: 0,
				PublishedAt: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC),
			},
			{
				ID: 5, Slug: "ai-startup-funding-mno345", URL: "https://ex.com/5",
				Title: "AI startup funding cools but concentrates", SourceName: "PitchBook", Topic: "business",
				TLDR: "Fewer rounds, bigger checks, flowing to a handful of infrastructure players.",
				WhyItMatters: "The land-grab phase is ending; moats and margins now decide winners.",
				Score:        0.79, Rank: 1,
				PublishedAt: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
			},
		},
		editions: append(makeEditions("technical", 12), makeEditions("business", 4)...),
	}
	return NewServer(fs, r, Config{SiteName: "AI News", BaseURL: "https://news.example.com", Description: "desc"}, nil)
}

// fakeSender records subscribe calls for the subscription tests.
type fakeSender struct {
	subscribed []string
	fail       bool
}

func (f *fakeSender) Name() string { return "fake" }
func (f *fakeSender) Subscribe(_ context.Context, email string) error {
	if f.fail {
		return errFakeSender
	}
	f.subscribed = append(f.subscribed, email)
	return nil
}
func (f *fakeSender) Broadcast(context.Context, string, string) error { return nil }

var errFakeSender = errorString("sender down")

type errorString string

func (e errorString) Error() string { return string(e) }

func newTestServerWithSender(t *testing.T, sender *fakeSender) *Server {
	t.Helper()
	s := newTestServer(t)
	s.sender = sender
	return s
}

func get(t *testing.T, s *Server, path string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	res := rec.Result()
	return res, rec.Body.String()
}

func TestRoutesRender(t *testing.T) {
	s := newTestServer(t)
	tests := []struct {
		path         string
		wantStatus   int
		wantContains []string
	}{
		{"/", 200, []string{"AI News", "GPT-X launch", `rel="canonical"`}},
		{"/editions/technical-2026-07-18", 200, []string{"Technical · 18 Jul 2026", `href="/articles/gpt-x-launch-abc123"`}},
		{"/articles/gpt-x-launch-abc123", 200, []string{"GPT-X launch", "application/ld+json", "NewsArticle", "price/performance baseline"}},
		{"/topics/technical", 200, []string{"Technical · 18 Jul 2026", "Past editions", "GPT-X launch"}},
		{"/search?q=gpt", 200, []string{"result(s)", "GPT-X launch"}},
		{"/feed.xml", 200, []string{"<rss", "GPT-X launch", "https://news.example.com/articles/gpt-x-launch-abc123"}},
		{"/sitemap.xml", 200, []string{"<urlset", "/editions/technical-2026-07-18", "/articles/gpt-x-launch-abc123"}},
		{"/robots.txt", 200, []string{"Sitemap: https://news.example.com/sitemap.xml"}},
		{"/healthz", 200, []string{"ok"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			res, body := get(t, s, tt.path)
			if res.StatusCode != tt.wantStatus {
				t.Fatalf("%s: status = %d, want %d", tt.path, res.StatusCode, tt.wantStatus)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(body, want) {
					t.Errorf("%s: body missing %q", tt.path, want)
				}
			}
		})
	}
}

func TestTopicShowsLatestEditionWithPicker(t *testing.T) {
	s := newTestServer(t) // technical: 12 editions, newest = 18 Jul 2026

	res, body := get(t, s, "/topics/technical")
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	// Renders the latest edition's content directly, not a bare date list.
	if !strings.Contains(body, "GPT-X launch") {
		t.Error("topic page should show the latest edition's articles")
	}
	// The "Past editions" picker lets the reader jump to older editions.
	if !strings.Contains(body, "Past editions") {
		t.Error("topic page should render the Past editions picker")
	}
	if !strings.Contains(body, `href="/editions/technical-2026-07-09"`) {
		t.Error("picker should link to older editions")
	}
	// Canonical points at the dated permalink, not the topic URL.
	if !strings.Contains(body, `rel="canonical" href="https://news.example.com/editions/technical-2026-07-18"`) {
		t.Error("topic view canonical should be the edition permalink")
	}
}

func TestEmptyTopic(t *testing.T) {
	s := newTestServer(t)
	res, body := get(t, s, "/topics/unknown")
	if res.StatusCode != 200 {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if !strings.Contains(body, "Nothing published") {
		t.Error("empty topic should show a friendly message")
	}
}

func postForm(t *testing.T, s *Server, path string, form url.Values) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec.Result()
}

func TestSubscribeValid(t *testing.T) {
	sender := &fakeSender{}
	s := newTestServerWithSender(t, sender)
	res := postForm(t, s, "/api/subscribe", url.Values{"email": {"reader@example.com"}})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/subscribed?status=ok" {
		t.Errorf("location = %q", loc)
	}
	if len(sender.subscribed) != 1 || sender.subscribed[0] != "reader@example.com" {
		t.Errorf("subscribed = %v", sender.subscribed)
	}
}

func TestSubscribeInvalidEmail(t *testing.T) {
	sender := &fakeSender{}
	s := newTestServerWithSender(t, sender)
	res := postForm(t, s, "/api/subscribe", url.Values{"email": {"not-an-email"}})
	if res.Header.Get("Location") != "/subscribed?status=invalid" {
		t.Errorf("location = %q", res.Header.Get("Location"))
	}
	if len(sender.subscribed) != 0 {
		t.Error("invalid email should not reach the sender")
	}
}

func TestSubscribeUnavailableWhenNoSender(t *testing.T) {
	s := newTestServer(t) // sender nil
	res := postForm(t, s, "/api/subscribe", url.Values{"email": {"a@b.com"}})
	if res.Header.Get("Location") != "/subscribed?status=unavailable" {
		t.Errorf("location = %q", res.Header.Get("Location"))
	}
}

func TestSubscribeRateLimited(t *testing.T) {
	s := newTestServerWithSender(t, &fakeSender{})
	var last *http.Response
	for i := 0; i < 7; i++ {
		last = postForm(t, s, "/api/subscribe", url.Values{"email": {"a@b.com"}})
	}
	if last.Header.Get("Location") != "/subscribed?status=ratelimited" {
		t.Errorf("expected rate limit after repeated attempts, got %q", last.Header.Get("Location"))
	}
}

func TestSubscribedPage(t *testing.T) {
	s := newTestServer(t)
	res, body := get(t, s, "/subscribed?status=ok")
	if res.StatusCode != 200 || !strings.Contains(body, "Check your inbox") {
		t.Errorf("status=%d body missing confirmation", res.StatusCode)
	}
	res2, _ := get(t, s, "/subscribed?status=invalid")
	if res2.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid status page = %d, want 400", res2.StatusCode)
	}
}

func TestArticleNotFound(t *testing.T) {
	s := newTestServer(t)
	res, _ := get(t, s, "/articles/does-not-exist")
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
}

func TestCanonicalURL(t *testing.T) {
	s := newTestServer(t)
	if got := s.canonical("/topics/technical"); got != "https://news.example.com/topics/technical" {
		t.Fatalf("canonical = %q", got)
	}
	if got := s.canonical("/"); got != "https://news.example.com/" {
		t.Fatalf("canonical root = %q", got)
	}
}
