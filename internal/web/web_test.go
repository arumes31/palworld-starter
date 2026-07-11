package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"arumes31/palworld-starter/internal/game"
	"arumes31/palworld-starter/internal/state"
)

const testTemplateDir = "../../templates"

func newTestServer(t *testing.T) *Server {
	t.Helper()
	st := state.New(filepath.Join(t.TempDir(), "time_remaining.json"))
	return New(game.NewController("palworld_test_container"), st, testTemplateDir, "../../static")
}

func TestSessionEncryptDecrypt(t *testing.T) {
	val := []byte(`{"captcha_answer":123,"language":"de","csrf_token":"abcde"}`)
	encrypted, err := encryptSession(val)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	decrypted, err := decryptSession(encrypted)
	if err != nil {
		t.Fatalf("Decryption failed: %v", err)
	}

	if string(decrypted) != string(val) {
		t.Errorf("Decrypted value mismatch: %s; expected %s", string(decrypted), string(val))
	}

	// Corrupted cipher
	corrupted := encrypted + "x"
	_, err = decryptSession(corrupted)
	if err == nil {
		t.Errorf("Decryption should have failed for corrupted ciphertext")
	}

	// Invalid format
	_, err = decryptSession("invalid")
	if err == nil {
		t.Errorf("Decryption should have failed for invalid ciphertext")
	}
}

func TestCSRFValidation(t *testing.T) {
	srv := newTestServer(t)

	// 1. Request with invalid CSRF token
	req := httptest.NewRequest("POST", "/start", strings.NewReader("csrf_token=wrong&captcha_answer=100"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	sessionData := &SessionData{
		CaptchaAnswer: 100,
		CsrfToken:     "correct_csrf",
		Language:      "en",
	}
	sessionBytes, _ := json.Marshal(sessionData)
	signedSession, _ := encryptSession(sessionBytes)
	req.AddCookie(&http.Cookie{Name: "session", Value: signedSession})

	rr := httptest.NewRecorder()
	srv.handleStart(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for bad CSRF, got %d", rr.Code)
	}

	// 2. Request with correct CSRF and correct answer
	form := url.Values{}
	form.Set("csrf_token", "correct_csrf")
	form.Set("captcha_answer", "100")
	reqCorrect := httptest.NewRequest("POST", "/start", strings.NewReader(form.Encode()))
	reqCorrect.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqCorrect.AddCookie(&http.Cookie{Name: "session", Value: signedSession})

	rrCorrect := httptest.NewRecorder()
	srv.handleStart(rrCorrect, reqCorrect)

	// Docker start fails or is skipped in tests, so the handler redirects (303).
	if rrCorrect.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect (303), got %d", rrCorrect.Code)
	}
}

func TestHandlePlayers(t *testing.T) {
	// The test container is never running, so the handler must answer with
	// zero players and joinable=false without touching the game's REST API.
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/api/players", nil)
	rr := httptest.NewRecorder()
	srv.handlePlayers(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected application/json, got %q", ct)
	}

	var body struct {
		Status   string            `json:"status"`
		Count    int               `json:"count"`
		Players  []game.PlayerInfo `json:"players"`
		Joinable bool              `json:"joinable"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}
	if body.Count != 0 || len(body.Players) != 0 {
		t.Errorf("Expected empty player list, got count=%d players=%v", body.Count, body.Players)
	}
	if body.Players == nil {
		t.Errorf("players must be an empty array, not null")
	}
	if body.Joinable {
		t.Errorf("joinable must be false while the container is not running")
	}
}

func TestHandleHealthz(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	srv.handleHealthz(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("Invalid JSON response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("Expected status ok, got %v", body["status"])
	}
	if _, ok := body["container"]; !ok {
		t.Errorf("Expected container field in healthz response")
	}
}

func TestHandleCaptchaImage(t *testing.T) {
	srv := newTestServer(t)

	// Without a session there are no captcha numbers → 404, no image leak.
	req := httptest.NewRequest("GET", "/captcha/num?i=1", nil)
	rr := httptest.NewRecorder()
	srv.handleCaptchaImage(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("Expected 404 without session numbers, got %d", rr.Code)
	}

	// With numbers in the session the endpoint serves a PNG.
	sessionData := &SessionData{CaptchaNum1: 142, CaptchaNum2: 37, Language: "en"}
	sessionBytes, _ := json.Marshal(sessionData)
	signedSession, _ := encryptSession(sessionBytes)

	req2 := httptest.NewRequest("GET", "/captcha/num?i=2", nil)
	req2.AddCookie(&http.Cookie{Name: "session", Value: signedSession})
	rr2 := httptest.NewRecorder()
	srv.handleCaptchaImage(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("Expected 200 for session-backed image, got %d", rr2.Code)
	}
	if ct := rr2.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Expected image/png, got %q", ct)
	}
	if !strings.HasPrefix(rr2.Body.String(), "\x89PNG") {
		t.Errorf("Response body is not a PNG")
	}
}

func TestIndexRendersPlayersSection(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	srv.handleIndex(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rr.Code)
	}
	html := rr.Body.String()
	for _, want := range []string{`id="player-count"`, `id="player-list"`, "/api/players", "join-help", "80.66.59.216:8211"} {
		if !strings.Contains(html, want) {
			t.Errorf("Index page missing %q", want)
		}
	}
}

// TestAllTemplatesRender guards against a bad deploy 500-ing every page: each
// content template must parse together with base.html and execute in both
// languages.
func TestAllTemplatesRender(t *testing.T) {
	srv := newTestServer(t)

	entries, err := os.ReadDir(testTemplateDir)
	if err != nil {
		t.Fatalf("Cannot read template dir: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "base.html" || !strings.HasSuffix(name, ".html") {
			continue
		}
		for _, lang := range []string{"de", "en"} {
			rr := httptest.NewRecorder()
			srv.renderTemplate(rr, name, PageContext{
				Language:            lang,
				DockerContainerName: "Test Server",
				Status:              "running",
				ServerAddress:       "1.2.3.4:8211",
				BootEstimateSeconds: BootEstimateSeconds,
			})
			if rr.Code != http.StatusOK {
				t.Errorf("Template %s (%s) failed to render: %d %s", name, lang, rr.Code, rr.Body.String())
			}
			if rr.Body.Len() == 0 {
				t.Errorf("Template %s (%s) rendered an empty page", name, lang)
			}
			if name == "index.html" && !strings.Contains(rr.Body.String(), "steam://connect/1.2.3.4:8211") {
				t.Errorf("Running index (%s) must contain the Steam connect link", lang)
			}
		}
	}
}
