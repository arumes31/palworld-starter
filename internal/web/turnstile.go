package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// turnstileVerifyURL is Cloudflare's server-side token validation endpoint.
const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// turnstile is the optional Cloudflare Turnstile bot check that runs on top of
// the arithmetic captcha. It is enabled when both TURNSTILE_SITE_KEY and
// TURNSTILE_SECRET_KEY are set; the arithmetic puzzle alone can be solved by a
// determined script, a Turnstile token cannot be minted without Cloudflare.
type turnstile struct {
	siteKey   string
	secret    string
	verifyURL string
	client    *http.Client
}

// newTurnstileFromEnv returns the configured check, or nil when disabled.
func newTurnstileFromEnv() *turnstile {
	site := strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY"))
	secret := strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY"))
	if site == "" && secret == "" {
		return nil
	}
	if site == "" || secret == "" {
		log.Fatalf("TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY must be set together")
	}
	return &turnstile{
		siteKey:   site,
		secret:    secret,
		verifyURL: turnstileVerifyURL,
		client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// verify validates the widget token submitted with r. Any failure, including
// Cloudflare being unreachable, rejects the request.
func (t *turnstile) verify(r *http.Request, remoteIP string) bool {
	token := r.FormValue("cf-turnstile-response")
	if token == "" || len(token) > 2048 {
		return false
	}
	form := url.Values{"secret": {t.secret}, "response": {token}}
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.verifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.client.Do(req)
	if err != nil {
		log.Printf("turnstile: siteverify failed: %v", err)
		return false
	}
	defer resp.Body.Close()
	var result struct {
		Success    bool     `json:"success"`
		ErrorCodes []string `json:"error-codes"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&result) != nil {
		log.Printf("turnstile: unexpected siteverify response (HTTP %d)", resp.StatusCode)
		return false
	}
	if !result.Success {
		log.Printf("turnstile: token rejected: %v", result.ErrorCodes)
	}
	return result.Success
}
