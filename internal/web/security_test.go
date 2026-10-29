package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captchaPost submits a captcha answer to /add_time with the given session.
func captchaPost(srv *Server, sd *SessionData, answer int) *httptest.ResponseRecorder {
	b, _ := json.Marshal(sd)
	enc, _ := encryptSession(b)
	form := url.Values{"csrf_token": {sd.CsrfToken}, "captcha_answer": {strconv.Itoa(answer)}}
	req := httptest.NewRequest(http.MethodPost, "/add_time", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: enc})
	rr := httptest.NewRecorder()
	srv.handleAddTime(rr, req)
	return rr
}

func TestCaptchaCannotBeReplayed(t *testing.T) {
	srv := newTestServer(t)
	sd := &SessionData{
		CaptchaID: srv.captchas.issue(pendingCaptcha{answer: 150, num1: 120, num2: 30, server: "test"}),
		CsrfToken: "tok",
	}

	if rr := captchaPost(srv, sd, 150); rr.Header().Get("Location") != "/" {
		t.Fatalf("first correct answer = %d %q, want redirect to /", rr.Code, rr.Header().Get("Location"))
	}
	// Re-sending the very same cookie with the known answer must fail.
	if rr := captchaPost(srv, sd, 150); !strings.HasPrefix(rr.Header().Get("Location"), "/captcha_error") {
		t.Fatalf("replayed captcha = %q, want captcha_error", rr.Header().Get("Location"))
	}
}

func TestWrongCaptchaAnswerConsumesChallenge(t *testing.T) {
	srv := newTestServer(t)
	sd := &SessionData{
		CaptchaID: srv.captchas.issue(pendingCaptcha{answer: 150, num1: 120, num2: 30, server: "test"}),
		CsrfToken: "tok",
	}
	if rr := captchaPost(srv, sd, 149); !strings.HasPrefix(rr.Header().Get("Location"), "/captcha_error") {
		t.Fatalf("wrong answer = %q, want captcha_error", rr.Header().Get("Location"))
	}
	// The challenge is gone, so the right answer no longer helps.
	if rr := captchaPost(srv, sd, 150); !strings.HasPrefix(rr.Header().Get("Location"), "/captcha_error") {
		t.Fatalf("answer after a wrong guess = %q, want captcha_error", rr.Header().Get("Location"))
	}
}

func TestCaptchaFailuresLockOutClient(t *testing.T) {
	srv := newTestServer(t)
	for i := 0; i < 11; i++ {
		sd := &SessionData{
			CaptchaID: srv.captchas.issue(pendingCaptcha{answer: 150, server: "test"}),
			CsrfToken: "tok",
		}
		captchaPost(srv, sd, 1)
	}
	sd := &SessionData{
		CaptchaID: srv.captchas.issue(pendingCaptcha{answer: 150, server: "test"}),
		CsrfToken: "tok",
	}
	if rr := captchaPost(srv, sd, 150); !strings.HasPrefix(rr.Header().Get("Location"), "/captcha_error") {
		t.Fatalf("locked-out client solved a captcha: %q", rr.Header().Get("Location"))
	}
}

func TestFailureLimiterBackoff(t *testing.T) {
	l := newFailureLimiter(2, time.Minute, 10*time.Minute, time.Hour)
	now := time.Now()
	l.fail("ip", now)
	l.fail("ip", now)
	if l.blocked("ip", now) != 0 {
		t.Fatal("blocked before free failures were used up")
	}
	l.fail("ip", now)
	if d := l.blocked("ip", now); d != time.Minute {
		t.Fatalf("first lockout = %v, want 1m", d)
	}
	l.fail("ip", now)
	if d := l.blocked("ip", now); d != 2*time.Minute {
		t.Fatalf("second lockout = %v, want 2m", d)
	}
	for i := 0; i < 50; i++ {
		l.fail("ip", now)
	}
	if d := l.blocked("ip", now); d != 10*time.Minute {
		t.Fatalf("capped lockout = %v, want 10m", d)
	}
	if l.blocked("other", now) != 0 {
		t.Error("lockout leaked to another key")
	}
	l.reset("ip")
	if l.blocked("ip", now) != 0 {
		t.Error("reset did not clear the lockout")
	}
}

func TestAddTimeIsCapped(t *testing.T) {
	srv := newTestServer(t)
	inst := srv.instances[0]
	inst.State.SetTimeRemaining(maxTimeRemaining - 60)
	if got := inst.State.UpdateTimeRemaining(extendTime); got != maxTimeRemaining {
		t.Errorf("capped time = %d, want %d", got, maxTimeRemaining)
	}
	if got := extendTime(3600); got != 3600+43200 {
		t.Errorf("extendTime(1h) = %d, want 13h", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for header, want := range map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
	} {
		if got := rr.Header().Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if csp := rr.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP missing frame-ancestors: %q", csp)
	}
}

func TestSessionCookieFlags(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "")
	rr := httptest.NewRecorder()
	saveSession(rr, httptest.NewRequest(http.MethodGet, "/", nil), &SessionData{})
	c := rr.Result().Cookies()[0]
	if c.SameSite != http.SameSiteLaxMode || !c.HttpOnly || c.Secure {
		t.Errorf("plain HTTP cookie = SameSite %v HttpOnly %v Secure %v", c.SameSite, c.HttpOnly, c.Secure)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr = httptest.NewRecorder()
	saveSession(rr, req, &SessionData{})
	if !rr.Result().Cookies()[0].Secure {
		t.Error("cookie behind a TLS proxy is not Secure")
	}

	t.Setenv("COOKIE_SECURE", "true")
	rr = httptest.NewRecorder()
	saveSession(rr, httptest.NewRequest(http.MethodGet, "/", nil), &SessionData{})
	if !rr.Result().Cookies()[0].Secure {
		t.Error("COOKIE_SECURE=true did not force Secure")
	}
}

func TestAllowedHostsRestrictSiteURL(t *testing.T) {
	srv := newTestServer(t)
	t.Setenv("WEBSITE_URL", "")
	t.Setenv("ALLOWED_HOSTS", "pal.example.com, www.pal.example.com")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.example.net"
	if got := srv.siteURL(req); got != "http://pal.example.com/" {
		t.Errorf("forged host siteURL = %q", got)
	}
	req.Host = "www.pal.example.com"
	if got := srv.siteURL(req); got != "http://www.pal.example.com/" {
		t.Errorf("allowed host siteURL = %q", got)
	}
}

func TestAdminLogoutRevokesSession(t *testing.T) {
	srv, _ := newAdminServer(t, "secret-long-enough")
	cookie := adminCookie(t, srv.admin, "secret-long-enough", "tok")

	get := func() int {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/api/players?srv=alpha", nil)
		req.AddCookie(cookie)
		srv.handleAdminPlayers(rr, req)
		return rr.Code
	}
	if code := get(); code == http.StatusUnauthorized {
		t.Fatal("fresh admin session rejected")
	}

	if code := adminLogout(srv, cookie, http.MethodPost, "tok"); code != http.StatusSeeOther {
		t.Fatalf("logout = %d, want 303", code)
	}

	// The old cookie value is still replayed after logout.
	if code := get(); code != http.StatusUnauthorized {
		t.Errorf("revoked session = %d, want 401", code)
	}
}

// adminLogout sends a logout request and returns the status code.
func adminLogout(srv *Server, cookie *http.Cookie, method, csrf string) int {
	form := url.Values{"csrf_token": {csrf}}
	req := httptest.NewRequest(method, "/admin/logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	srv.Routes().ServeHTTP(rr, req)
	return rr.Code
}

func TestAdminLogoutRequiresPostAndCsrf(t *testing.T) {
	srv, _ := newAdminServer(t, "secret-long-enough")
	cookie := adminCookie(t, srv.admin, "secret-long-enough", "tok")

	if code := adminLogout(srv, cookie, http.MethodGet, "tok"); code != http.StatusMethodNotAllowed {
		t.Errorf("GET logout = %d, want 405", code)
	}
	if code := adminLogout(srv, cookie, http.MethodPost, "wrong"); code != http.StatusForbidden {
		t.Errorf("logout with bad csrf = %d, want 403", code)
	}
	if code := adminLogout(srv, cookie, http.MethodPost, ""); code != http.StatusForbidden {
		t.Errorf("logout without csrf = %d, want 403", code)
	}
	// None of the rejected requests may have revoked the session.
	if srv.revoked.revoked("test-session") {
		t.Error("rejected logout revoked the session")
	}
}

func TestAdminLogoutSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin-revoked.json")
	srv, instances := newAdminServer(t, "secret-long-enough")
	if err := srv.PersistRevocations(path); err != nil {
		t.Fatal(err)
	}
	cookie := adminCookie(t, srv.admin, "secret-long-enough", "tok")
	if code := adminLogout(srv, cookie, http.MethodPost, "tok"); code != http.StatusSeeOther {
		t.Fatalf("logout = %d, want 303", code)
	}

	// Rebuild the web server with the same key, credentials and state dir.
	restarted := New(instances, testTemplateDir, "../../static", srv.admin)
	if err := restarted.PersistRevocations(path); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/players?srv=alpha", nil)
	req.AddCookie(cookie)
	restarted.handleAdminPlayers(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("revoked session after restart = %d, want 401", rr.Code)
	}
}

func TestAdminLoginLockout(t *testing.T) {
	srv, _ := newAdminServer(t, "secret-long-enough")
	sd := &SessionData{CsrfToken: "tok"}
	b, _ := json.Marshal(sd)
	enc, _ := encryptSession(b)

	login := func(pw string) string {
		form := url.Values{"csrf_token": {"tok"}, "password": {pw}}
		req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(&http.Cookie{Name: "session", Value: enc})
		rr := httptest.NewRecorder()
		srv.handleAdminLogin(rr, req)
		return rr.Header().Get("Location")
	}
	for i := 0; i < 6; i++ {
		login("wrong")
	}
	if loc := login("secret-long-enough"); loc != "/admin/login?err=locked" {
		t.Errorf("login during lockout = %q, want locked", loc)
	}
}

func TestRevocationPersistFailures(t *testing.T) {
	// An unwritable state path is refused up front.
	missing := filepath.Join(t.TempDir(), "no-such-dir", "admin-revoked.json")
	if err := newRevocationList().persistTo(missing); err == nil {
		t.Error("persistTo accepted an unwritable path")
	}

	// A write that fails at logout time is reported, not swallowed.
	dir := t.TempDir()
	srv, _ := newAdminServer(t, "secret-long-enough")
	if err := srv.PersistRevocations(filepath.Join(dir, "admin-revoked.json")); err != nil {
		t.Fatal(err)
	}
	srv.revoked.path = filepath.Join(dir, "gone", "admin-revoked.json")
	cookie := adminCookie(t, srv.admin, "secret-long-enough", "tok")
	if code := adminLogout(srv, cookie, http.MethodPost, "tok"); code != http.StatusInternalServerError {
		t.Errorf("logout with failed persistence = %d, want 500", code)
	}
	// The session is still revoked for this process.
	if !srv.revoked.revoked("test-session") {
		t.Error("failed persistence left the session usable")
	}
}
