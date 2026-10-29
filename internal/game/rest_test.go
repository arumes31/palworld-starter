package game

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRESTClientRefusesRedirects(t *testing.T) {
	var hits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer target.Close()
	game := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/containers/x/stop", http.StatusTemporaryRedirect)
	}))
	defer game.Close()

	c := &Controller{apiBase: game.URL + "/v1/api", adminPassword: "pw"}
	err := c.SaveWorld()
	if !errors.Is(err, errRESTRedirect) {
		t.Fatalf("SaveWorld error = %v, want redirect refusal", err)
	}
	if hits.Load() != 0 {
		t.Error("redirect target was contacted")
	}
}

func TestRESTBaseURL(t *testing.T) {
	cases := []struct {
		raw, host string
		port      int
		want      string
		wantErr   bool
	}{
		{"", "palworld", 8212, "http://palworld:8212/v1/api", false},
		{"", "", 8212, "http://localhost:8212/v1/api", false},
		{"", "::1", 8212, "http://[::1]:8212/v1/api", false},
		{"https://pal.example.com:8212", "ignored", 1, "https://pal.example.com:8212/v1/api", false},
		{"https://pal.example.com/proxy/v1/api/", "", 0, "https://pal.example.com/proxy/v1/api", false},
		{"ftp://pal.example.com", "", 0, "", true},
		{"https://user:pw@pal.example.com", "", 0, "", true},
		{"https://pal.example.com/?x=1", "", 0, "", true},
		{"https:///nohost", "", 0, "", true},
	}
	for _, tc := range cases {
		got, err := RESTBaseURL(tc.raw, tc.host, tc.port)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("RESTBaseURL(%q, %q, %d) = %q, %v; want %q, err=%v", tc.raw, tc.host, tc.port, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestIsLocalHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":            true,
		"127.0.0.1":            true,
		"10.1.2.3":             true,
		"192.168.0.5":          true,
		"palworld-server":      true,
		"host.docker.internal": true,
		"80.66.59.216":         false,
		"pal.example.com":      false,
	} {
		if got := isLocalHost(host); got != want {
			t.Errorf("isLocalHost(%q) = %v, want %v", host, got, want)
		}
	}
}
