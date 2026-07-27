package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
)

// This file is what makes the shelf safe to put behind a Cloudflare Tunnel.
//
// Karasu and the KOReader plugin authenticate with a bearer header, which never
// leaves a trace anywhere. A browser cannot set a header by following a link, so
// it used to pass the key as ?key= — and a query string is written down by the
// browser's history, by the tunnel's request log, and by anything in between.
// Instead the browser trades the key once at /login for a session cookie.

const (
	sessionCookie = "yata_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// sessions is in memory on purpose: a restart logging the browser out is a
// feature, and persisting session tokens would put a second credential on disk.
type sessions struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func newSessions() *sessions { return &sessions{m: map[string]time.Time{}} }

func (s *sessions) issue() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "" // crypto/rand failing means no session, never a guessable one
	}
	tok := base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for t, exp := range s.m { // opportunistic sweep; the map holds a handful
		if now.After(exp) {
			delete(s.m, t)
		}
	}
	s.m[tok] = now.Add(sessionTTL)
	return tok
}

func (s *sessions) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[tok]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.m, tok)
		return false
	}
	return true
}

func (s *sessions) drop(tok string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, tok)
}

// throttle slows key guessing down without ever locking anyone out.
//
// A per-IP limit is worthless behind a tunnel — every request arrives from
// cloudflared on localhost, and trusting X-Forwarded-For hands the attacker the
// bypass. So the delay is global and applies only after a guess is already
// known to be wrong: whoever holds the real key never waits.
type throttle struct {
	mu     sync.Mutex
	fails  int
	lastAt time.Time
}

func (t *throttle) penalty() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	// A minute without a wrong guess forgets the streak.
	if time.Since(t.lastAt) > time.Minute {
		t.fails = 0
	}
	t.lastAt = time.Now()
	t.fails++

	d := time.Duration(t.fails) * 250 * time.Millisecond
	if d > 3*time.Second {
		d = 3 * time.Second
	}
	return d
}

func (t *throttle) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fails = 0
}

// secure reports whether the browser reached us over TLS, so the session cookie
// is not marked Secure on a plain-HTTP LAN install (where it would be dropped)
// but is behind a tunnel, where the connection really is https.
func secure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

type gate struct {
	cfg  *config
	sess *sessions
	thr  *throttle
}

// ok reports whether the request carries a credential. A blank key is an
// intentionally open shelf (LAN behind a firewall), not a misconfiguration.
func (g *gate) ok(r *http.Request) bool {
	key := g.cfg.apiKey()
	if key == "" {
		return true
	}
	if h := r.Header.Get("Authorization"); h != "" {
		return subtle.ConstantTimeCompare([]byte(h), []byte("Bearer "+key)) == 1
	}
	c, err := r.Cookie(sessionCookie)
	return err == nil && g.sess.valid(c.Value)
}

func (g *gate) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		if g.ok(r) {
			next.ServeHTTP(w, r)
			return
		}
		time.Sleep(g.thr.penalty())
		// A browser following a link cannot set a header: give it the login
		// form instead of a dead end.
		if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
			askForKey(w, "")
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// handleLogin trades the key for a session cookie. It is the one route outside
// the gate, so it carries the throttle itself.
func (g *gate) handleLogin(w http.ResponseWriter, r *http.Request) {
	securityHeaders(w)
	key := g.cfg.apiKey()
	if key == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostFormValue("key")), []byte(key)) != 1 {
		time.Sleep(g.thr.penalty())
		w.WriteHeader(http.StatusUnauthorized)
		askForKey(w, "That key was not accepted.")
		return
	}
	g.thr.reset()

	tok := g.sess.issue()
	if tok == "" {
		http.Error(w, "cannot start a session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true, // no script ever needs to read it
		Secure:   secure(r),
		// Strict is the CSRF defence: a cross-site POST does not carry the
		// cookie at all, so no state-changing form can be forged.
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (g *gate) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		g.sess.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure(r), SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// securityHeaders is written on every response, including the 401s.
//
// The pages load nothing from anywhere: styles and the one script are inline,
// the favicon is a data URI. So default-src can be 'none' and any injected tag
// pointing outward is dead on arrival. Styles still need 'unsafe-inline';
// scripts do not, and get a nonce instead.
func securityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	// The pages carry no secret in their URLs any more, but a Referer to an
	// external host is still something this app never has a reason to emit.
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
}

// contentSecurityPolicy is set per response because the script nonce changes.
func contentSecurityPolicy(w http.ResponseWriter, nonce string) {
	w.Header().Set("Content-Security-Policy", strings.Join([]string{
		"default-src 'none'",
		"img-src data:",
		"style-src 'unsafe-inline'",
		"script-src 'nonce-" + nonce + "'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
	}, "; "))
}

func newNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(b)
}
