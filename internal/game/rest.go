package game

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// errRESTRedirect is returned for any redirect from the game REST API.
var errRESTRedirect = errors.New("game REST API redirect refused")

// restClient returns an HTTP client for the game REST API. Redirects are never
// followed: the API does not issue them, and following one would let a
// tampered response send the admin credentials, or a POST, to another origin.
func restClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRESTRedirect
		},
	}
}

// RESTBaseURL resolves the REST API base URL of a server. rawURL (e.g.
// "https://palworld.example.com:8212") takes precedence and may use https;
// HTTPS certificates are verified against the system roots (SSL_CERT_FILE /
// SSL_CERT_DIR add a private CA). Without it, plain http://host:port is used.
// The "/v1/api" path is appended when rawURL has no path.
func RESTBaseURL(rawURL, host string, port int) (string, error) {
	if rawURL == "" {
		if host == "" {
			host = "localhost"
		}
		return fmt.Sprintf("http://%s/v1/api", net.JoinHostPort(host, fmt.Sprint(port))), nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid REST URL %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("REST URL %q must use http or https", rawURL)
	}
	if u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("REST URL %q must be scheme://host[:port][/path] without credentials, query or fragment", rawURL)
	}
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		path = "/v1/api"
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// warnPlaintextREST logs when the admin password would cross a network that is
// not local in cleartext. HTTP stays allowed for loopback, private addresses
// and container/service names, where the hop never leaves the host or LAN.
func warnPlaintextREST(apiBase string) {
	u, err := url.Parse(apiBase)
	if err != nil || u.Scheme != "http" || isLocalHost(u.Hostname()) {
		return
	}
	log.Printf("WARNING: game REST API %s uses plain HTTP; the admin password is sent in cleartext. "+
		"Use an https:// REST URL, a private network or an encrypted tunnel.", u.Host)
}

// isLocalHost reports whether host is loopback, a private/link-local address,
// or a name that resolves only inside a host or container network.
func isLocalHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || !strings.Contains(host, ".") ||
		host == "host.docker.internal" ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home.arpa")
}
