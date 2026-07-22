package web

import (
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// rateLimiter is a tiny per-key fixed-window limiter for the subscribe endpoint.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

// handleSubscribe validates the email, rate-limits by client IP, and registers
// it with the email provider (which sends the double opt-in). It redirects to
// /subscribed with a status the user sees.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if s.sender == nil {
		redirectSubscribed(w, r, "unavailable")
		return
	}
	if !s.subLimiter.allow(clientIP(r)) {
		redirectSubscribed(w, r, "ratelimited")
		return
	}
	addr, err := mail.ParseAddress(strings.TrimSpace(r.FormValue("email")))
	if err != nil {
		redirectSubscribed(w, r, "invalid")
		return
	}
	if err := s.sender.Subscribe(r.Context(), addr.Address); err != nil {
		s.log.Error("subscribe failed", zap.Error(err))
		redirectSubscribed(w, r, "error")
		return
	}
	redirectSubscribed(w, r, "ok")
}

func redirectSubscribed(w http.ResponseWriter, r *http.Request, status string) {
	http.Redirect(w, r, "/subscribed?status="+status, http.StatusSeeOther)
}

// handleSubscribed renders the confirmation/feedback page.
func (s *Server) handleSubscribed(w http.ResponseWriter, r *http.Request) {
	heading, message := "Almost there", "Check your inbox to confirm your subscription."
	code := http.StatusOK
	switch r.URL.Query().Get("status") {
	case "ok":
	case "invalid":
		heading, message = "Invalid email", "That doesn't look like a valid email address — please try again."
		code = http.StatusBadRequest
	case "ratelimited":
		heading, message = "Slow down", "Too many attempts. Please try again in a minute."
		code = http.StatusTooManyRequests
	case "unavailable":
		heading, message = "Not available yet", "Email subscriptions aren't enabled yet. Check back soon."
	default:
		heading, message = "Something went wrong", "We couldn't sign you up right now. Please try again later."
		code = http.StatusInternalServerError
	}
	s.renderMessage(w, r, code, heading, message)
}

// clientIP returns the best-effort client IP, honoring X-Forwarded-For when set
// (the server runs behind a proxy in production).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
