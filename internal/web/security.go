package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// contentSecurityPolicy allows only same-origin resources plus Google Fonts
// and, when turnstile is set, Cloudflare's challenge script and frame. Inline
// scripts and styles are still used by the templates, so 'unsafe-inline' stays
// for now; framing this site is forbidden entirely.
func contentSecurityPolicy(turnstile bool) string {
	scripts, frames, connect := "'self' 'unsafe-inline'", "'none'", "'self'"
	if turnstile {
		const cf = "https://challenges.cloudflare.com"
		scripts += " " + cf
		frames = cf
		connect += " " + cf
	}
	return "default-src 'self'; " +
		"script-src " + scripts + "; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com; " +
		"img-src 'self' data: blob:; " +
		"connect-src " + connect + "; " +
		"frame-src " + frames + "; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'"
}

// securityHeaders adds clickjacking, sniffing and referrer protection to every
// response.
func securityHeaders(next http.Handler, csp string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the address used for rate limiting. X-Forwarded-For is only
// honoured when TRUST_PROXY_HEADERS is set, because without a reverse proxy in
// front any client could spoof it. The rightmost entry is the one appended by
// the proxy directly in front of us.
func (s *Server) clientIP(r *http.Request) string {
	if s.trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// randomToken returns n random bytes hex-encoded.
func randomToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// cookieSecure decides the Secure flag of the session cookie. COOKIE_SECURE
// forces it on ("true") or off ("false"); otherwise it is set whenever the
// request arrived over TLS, directly or via a reverse proxy.
func cookieSecure(r *http.Request) bool {
	switch strings.ToLower(os.Getenv("COOKIE_SECURE")) {
	case "true", "1", "yes":
		return true
	case "false", "0", "no":
		return false
	}
	if r == nil {
		return false
	}
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// limiterMaxEntries bounds the memory a flood of distinct keys can consume.
const limiterMaxEntries = 10000

// failureLimiter locks a key out after repeated failures, doubling the lockout
// with every further failure. Failures are forgotten after window without new
// ones.
type failureLimiter struct {
	mu      sync.Mutex
	free    int           // failures allowed before lockouts start
	base    time.Duration // first lockout
	maxLock time.Duration // lockout cap
	window  time.Duration // idle time after which failures are forgotten
	entries map[string]*failureEntry
}

type failureEntry struct {
	fails       int
	last        time.Time
	lockedUntil time.Time
}

func newFailureLimiter(free int, base, maxLock, window time.Duration) *failureLimiter {
	return &failureLimiter{
		free:    free,
		base:    base,
		maxLock: maxLock,
		window:  window,
		entries: make(map[string]*failureEntry),
	}
}

// blocked returns how long key is still locked out (0 when it is not).
func (l *failureLimiter) blocked(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil || !now.Before(e.lockedUntil) {
		return 0
	}
	return e.lockedUntil.Sub(now)
}

// fail records a failure for key.
func (l *failureLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		if len(l.entries) >= limiterMaxEntries {
			l.pruneLocked(now)
		}
		e = &failureEntry{}
		l.entries[key] = e
	} else if now.Sub(e.last) > l.window {
		e.fails = 0
	}
	e.fails++
	e.last = now
	if over := e.fails - l.free; over > 0 {
		lock := l.maxLock
		if over <= 20 { // beyond this the shift could overflow
			if d := l.base << (over - 1); d < l.maxLock {
				lock = d
			}
		}
		e.lockedUntil = now.Add(lock)
	}
}

// reset forgets all failures of key, e.g. after a successful login.
func (l *failureLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// pruneLocked drops idle entries; if the map is still full the oldest-looking
// entries go too, so an address flood cannot grow memory without bound.
func (l *failureLimiter) pruneLocked(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.last) > l.window && !now.Before(e.lockedUntil) {
			delete(l.entries, k)
		}
	}
	for k := range l.entries {
		if len(l.entries) < limiterMaxEntries {
			break
		}
		delete(l.entries, k)
	}
}

// revocationList remembers logged-out admin session ids until they expire on
// their own, so a copied cookie stops working after logout. With a path set the
// list is persisted, because a stable SESSION_KEY keeps cookies valid across
// restarts and a forgotten revocation would otherwise revive the session.
type revocationList struct {
	mu   sync.Mutex
	ids  map[string]int64 // session id -> unix expiry
	path string           // persistence file; empty keeps the list in memory
}

func newRevocationList() *revocationList {
	return &revocationList{ids: make(map[string]int64)}
}

// persistTo loads previously revoked ids from path and saves every later
// revocation there. A missing file is not an error, but path must be writable:
// the list is written back immediately, so a read-only or missing state dir
// fails at startup instead of silently losing revocations at logout.
func (rl *revocationList) persistTo(path string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.path = path
	data, err := os.ReadFile(path) // #nosec G304 -- operator-configured state path
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		var stored map[string]int64
		if err := json.Unmarshal(data, &stored); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		now := time.Now().Unix()
		for id, exp := range stored {
			if exp > now {
				rl.ids[id] = exp
			}
		}
	}
	return rl.saveLocked()
}

func (rl *revocationList) revoke(id string, expires int64) error {
	if id == "" {
		return nil
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now().Unix()
	for k, exp := range rl.ids {
		if exp <= now {
			delete(rl.ids, k)
		}
	}
	rl.ids[id] = expires
	return rl.saveLocked()
}

// saveLocked writes the list atomically (temp file + rename).
func (rl *revocationList) saveLocked() error {
	if rl.path == "" {
		return nil
	}
	data, err := json.Marshal(rl.ids)
	if err != nil {
		return err
	}
	tmp := rl.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, rl.path)
}

func (rl *revocationList) revoked(id string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	_, ok := rl.ids[id]
	return ok
}
