package web

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

var sessionKey []byte

func init() {
	sessionKey = make([]byte, 32)
	if _, err := rand.Read(sessionKey); err != nil {
		log.Fatalf("Failed to generate random session key: %v", err)
	}
}

// SessionData is the encrypted per-visitor session stored in a cookie.
type SessionData struct {
	CaptchaAnswer int    `json:"captcha_answer"`
	CaptchaNum1   int    `json:"captcha_num1"`
	CaptchaNum2   int    `json:"captcha_num2"`
	Language      string `json:"language"`
	CsrfToken     string `json:"csrf_token"`
}

func getSession(r *http.Request) *SessionData {
	cookie, err := r.Cookie("session")
	if err != nil {
		return &SessionData{Language: getPreferredLanguage(r)}
	}

	decoded, err := decryptSession(cookie.Value)
	if err != nil {
		return &SessionData{Language: getPreferredLanguage(r)}
	}

	var data SessionData
	if err := json.Unmarshal(decoded, &data); err != nil {
		return &SessionData{Language: getPreferredLanguage(r)}
	}
	return &data
}

func saveSession(w http.ResponseWriter, data *SessionData) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	encrypted, err := encryptSession(bytes)
	if err != nil {
		log.Printf("Session encryption failed: %v", err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    encrypted,
		Path:     "/",
		HttpOnly: true,
	})
}

func encryptSession(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func decryptSession(ciphertextStr string) ([]byte, error) {
	ciphertext, err := base64.RawURLEncoding.DecodeString(ciphertextStr)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := ciphertext[:nonceSize]
	actualCiphertext := ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, actualCiphertext, nil)
}

func getPreferredLanguage(r *http.Request) string {
	// Check query param first
	if l := r.URL.Query().Get("lang"); l == "de" || l == "en" {
		return l
	}
	accept := r.Header.Get("Accept-Language")
	if strings.Contains(accept, "de") {
		return "de"
	}
	return "en"
}
