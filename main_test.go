package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNumberToWords(t *testing.T) {
	tests := []struct {
		num      int
		lang     string
		expected string
	}{
		// German tests
		{0, "de", "null"},
		{1, "de", "eins"},
		{12, "de", "zwölf"},
		{20, "de", "zwanzig"},
		{21, "de", "einsundzwanzig"}, // Python code returned "einsundzwanzig"
		{99, "de", "neunundneunzig"},
		{100, "de", "einhundert"},
		{105, "de", "einhundertfünf"},
		{121, "de", "einhunderteinsundzwanzig"},
		{199, "de", "einhundertneunundneunzig"},

		// English tests
		{0, "en", "zero"},
		{1, "en", "one"},
		{12, "en", "twelve"},
		{20, "en", "twenty"},
		{21, "en", "twenty-one"},
		{99, "en", "ninety-nine"},
		{100, "en", "one hundred"},
		{105, "en", "one hundred five"},
		{121, "en", "one hundred twenty-one"},
		{199, "en", "one hundred ninety-nine"},
	}

	for _, tc := range tests {
		result := numberToWords(tc.num, tc.lang)
		if result != tc.expected {
			t.Errorf("numberToWords(%d, %q) = %q; expected %q", tc.num, tc.lang, result, tc.expected)
		}
	}
}

func TestGenerateCaptcha(t *testing.T) {
	for i := 0; i < 100; i++ {
		questionDe, answerDe := generateCaptcha("de")
		if questionDe == "" {
			t.Errorf("German captcha question should not be empty")
		}
		if answerDe < 1 || answerDe > 300 {
			t.Errorf("German captcha answer out of bounds: %d", answerDe)
		}

		questionEn, answerEn := generateCaptcha("en")
		if questionEn == "" {
			t.Errorf("English captcha question should not be empty")
		}
		if answerEn < 1 || answerEn > 300 {
			t.Errorf("English captcha answer out of bounds: %d", answerEn)
		}
	}
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
	// Initialize global state for testing
	globalState = &State{
		timeRemaining: 900,
	}

	// Set up server handler
	handler := handleStart("test_container")

	// 1. Request with invalid CSRF token
	req := httptest.NewRequest("POST", "/start", strings.NewReader("csrf_token=wrong&captcha_answer=100"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	// Create signed session cookie with matching answer but different CSRF
	sessionData := &SessionData{
		CaptchaAnswer: 100,
		CsrfToken:     "correct_csrf",
		Language:      "en",
	}
	sessionBytes, _ := json.Marshal(sessionData)
	signedSession, _ := encryptSession(sessionBytes)
	req.AddCookie(&http.Cookie{Name: "session", Value: signedSession})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

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
	handler.ServeHTTP(rrCorrect, reqCorrect)

	// Since we mock Docker (which fails gracefully or skips if client is nil),
	// the response should be a redirect to index (StatusSeeOther / 303)
	if rrCorrect.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect to index (303), got %d", rrCorrect.Code)
	}
}
