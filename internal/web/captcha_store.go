package web

import (
	"sync"
	"time"
)

const (
	// captchaTTL is how long an issued captcha may be answered.
	captchaTTL = 10 * time.Minute
	// captchaStoreLimit bounds the number of pending challenges in memory.
	captchaStoreLimit = 10000
)

// pendingCaptcha is a challenge issued to a visitor. It lives only on the
// server; the session cookie carries nothing but its random id, so a captcha
// can be answered exactly once and a solved cookie cannot be replayed.
type pendingCaptcha struct {
	answer  int
	num1    int
	num2    int
	server  string
	expires time.Time
}

// captchaStore holds pending challenges keyed by id.
type captchaStore struct {
	mu      sync.Mutex
	pending map[string]pendingCaptcha
}

func newCaptchaStore() *captchaStore {
	return &captchaStore{pending: make(map[string]pendingCaptcha)}
}

// issue stores a challenge and returns its id.
func (cs *captchaStore) issue(c pendingCaptcha) string {
	now := time.Now()
	c.expires = now.Add(captchaTTL)
	id := randomToken(16)

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if len(cs.pending) >= captchaStoreLimit {
		for k, p := range cs.pending {
			if now.After(p.expires) {
				delete(cs.pending, k)
			}
		}
		// Still full: evict arbitrary entries rather than grow unbounded.
		for k := range cs.pending {
			if len(cs.pending) < captchaStoreLimit {
				break
			}
			delete(cs.pending, k)
		}
	}
	cs.pending[id] = c
	return id
}

// peek returns a pending challenge without consuming it (for the images).
func (cs *captchaStore) peek(id string) (pendingCaptcha, bool) {
	if id == "" {
		return pendingCaptcha{}, false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c, ok := cs.pending[id]
	if !ok || time.Now().After(c.expires) {
		return pendingCaptcha{}, false
	}
	return c, true
}

// take removes and returns a challenge. Every submission consumes the
// challenge, right or wrong, so answers cannot be brute-forced against it.
func (cs *captchaStore) take(id string) (pendingCaptcha, bool) {
	if id == "" {
		return pendingCaptcha{}, false
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c, ok := cs.pending[id]
	if !ok {
		return pendingCaptcha{}, false
	}
	delete(cs.pending, id)
	if time.Now().After(c.expires) {
		return pendingCaptcha{}, false
	}
	return c, true
}
