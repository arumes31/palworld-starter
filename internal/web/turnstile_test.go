package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeTurnstile serves siteverify, accepting only the token "good".
func fakeTurnstile(t *testing.T) *turnstile {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("secret") != "secret" {
			t.Errorf("siteverify got secret %q", r.PostForm.Get("secret"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": r.PostForm.Get("response") == "good"})
	}))
	t.Cleanup(ts.Close)
	return &turnstile{siteKey: "site", secret: "secret", verifyURL: ts.URL, client: ts.Client()}
}

func turnstilePost(srv *Server, sd *SessionData, answer, token string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(sd)
	enc, _ := encryptSession(b)
	form := url.Values{"csrf_token": {sd.CsrfToken}, "captcha_answer": {answer}}
	if token != "" {
		form.Set("cf-turnstile-response", token)
	}
	req := httptest.NewRequest(http.MethodPost, "/add_time", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: enc})
	rr := httptest.NewRecorder()
	srv.handleAddTime(rr, req)
	return rr
}

func TestTurnstileRequiredWhenEnabled(t *testing.T) {
	srv := newTestServer(t)
	srv.turnstile = fakeTurnstile(t)

	for _, tc := range []struct {
		token string
		ok    bool
	}{{"", false}, {"bad", false}, {"good", true}} {
		sd := &SessionData{
			CaptchaID: srv.captchas.issue(pendingCaptcha{answer: 150, server: "test"}),
			CsrfToken: "tok",
		}
		loc := turnstilePost(srv, sd, "150", tc.token).Header().Get("Location")
		if got := loc == "/"; got != tc.ok {
			t.Errorf("token %q: Location %q, want success=%v", tc.token, loc, tc.ok)
		}
	}
}

func TestTurnstileCSPAndWidget(t *testing.T) {
	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/captcha", nil))
	if csp := rr.Header().Get("Content-Security-Policy"); strings.Contains(csp, "cloudflare") {
		t.Errorf("CSP allows Cloudflare while Turnstile is off: %s", csp)
	}
	if strings.Contains(rr.Body.String(), "cf-turnstile") {
		t.Error("widget rendered while Turnstile is off")
	}

	srv.turnstile = fakeTurnstile(t)
	rr = httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/captcha", nil))
	csp := rr.Header().Get("Content-Security-Policy")
	for _, want := range []string{"script-src 'self' 'unsafe-inline' https://challenges.cloudflare.com", "frame-src https://challenges.cloudflare.com"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP %q lacks %q", csp, want)
		}
	}
	if !strings.Contains(rr.Body.String(), `data-sitekey="site"`) {
		t.Error("widget not rendered while Turnstile is on")
	}
}

func TestSuccessfulPublicActionsAreLimited(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/add_time", nil)
	for i := 0; i < publicActionsPerHour; i++ {
		srv.recordPublicAction(req)
	}
	sd := &SessionData{
		CaptchaID: srv.captchas.issue(pendingCaptcha{answer: 150, server: "test"}),
		CsrfToken: "tok",
	}
	rr := captchaPost(srv, sd, 150)
	if rr.Code != http.StatusTooManyRequests || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("over-limit action = %d (Retry-After %q), want 429", rr.Code, rr.Header().Get("Retry-After"))
	}
	// The rejected request must not have burned the visitor's challenge.
	if _, ok := srv.captchas.peek(sd.CaptchaID); !ok {
		t.Error("rate-limited request consumed the captcha")
	}
}
