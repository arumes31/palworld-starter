package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStopAuthorized(t *testing.T) {
	cases := []struct {
		name    string
		token   string
		remote  string
		headers map[string]string
		want    bool
	}{
		{"loopback v4", "", "127.0.0.1:5555", nil, true},
		{"loopback v6", "", "[::1]:5555", nil, true},
		{"remote peer", "", "203.0.113.7:5555", nil, false},
		{"bridge peer", "", "172.17.0.1:5555", nil, false},
		{"proxied via loopback XFF", "", "127.0.0.1:5555", map[string]string{"X-Forwarded-For": "203.0.113.7"}, false},
		{"proxied via loopback X-Real-IP", "", "127.0.0.1:5555", map[string]string{"X-Real-IP": "203.0.113.7"}, false},
		{"proxied via loopback Forwarded", "", "[::1]:5555", map[string]string{"Forwarded": "for=203.0.113.7"}, false},
		{"token required, missing", "s3cret", "127.0.0.1:5555", nil, false},
		{"token required, wrong", "s3cret", "127.0.0.1:5555", map[string]string{"X-Stop-Token": "nope"}, false},
		{"token required, correct via proxy", "s3cret", "172.17.0.1:5555", map[string]string{"X-Stop-Token": "s3cret", "X-Forwarded-For": "203.0.113.7"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			srv.stopToken = tc.token
			req := httptest.NewRequest(http.MethodPost, "/stop", nil)
			req.RemoteAddr = tc.remote
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			if got := srv.stopAuthorized(req); got != tc.want {
				t.Errorf("stopAuthorized = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHandleStopRejectsProxiedLoopback(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/stop", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	rr := httptest.NewRecorder()
	srv.handleStop(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestSiteURLIgnoresForwardedHostWithoutTrust(t *testing.T) {
	t.Setenv("WEBSITE_URL", "")
	t.Setenv("ALLOWED_HOSTS", "")
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "example.org"
	req.Header.Set("X-Forwarded-Host", "evil.example")

	srv.trustProxy = false
	if got := srv.siteURL(req); strings.Contains(got, "evil") {
		t.Errorf("untrusted X-Forwarded-Host leaked into site URL: %q", got)
	}
	srv.trustProxy = true
	if got := srv.siteURL(req); got != "http://evil.example/" {
		t.Errorf("trusted X-Forwarded-Host not honoured: %q", got)
	}
}
