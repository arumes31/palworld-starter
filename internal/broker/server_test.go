package broker

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef"

type fakeBackend struct {
	action    string
	container string
}

func (b *fakeBackend) Status(_ context.Context, container string) (string, error) {
	b.action, b.container = "status", container
	return "running", nil
}
func (b *fakeBackend) Logs(_ context.Context, container string) (string, error) {
	b.action, b.container = "logs", container
	return "bounded logs", nil
}
func (b *fakeBackend) Start(_ context.Context, container string) error {
	b.action, b.container = "start", container
	return nil
}
func (b *fakeBackend) Stop(_ context.Context, container string) error {
	b.action, b.container = "stop", container
	return nil
}
func (b *fakeBackend) Backup(_ context.Context, container string) (int, string, error) {
	b.action, b.container = "backup", container
	return 0, "complete", nil
}

func newTestHandler(t *testing.T, backend Backend) http.Handler {
	t.Helper()
	server, err := NewServer(backend, testToken, []string{"palworld-server"})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

func brokerRequest(handler http.Handler, token, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestServerRequiresStrongConfiguration(t *testing.T) {
	backend := &fakeBackend{}
	if _, err := NewServer(backend, "weak", []string{"palworld-server"}); err == nil {
		t.Fatal("expected a weak token to be rejected")
	}
	if _, err := NewServer(backend, testToken, nil); err == nil {
		t.Fatal("expected an empty allowlist to be rejected")
	}
}

func TestServerEnforcesAuthenticationAndContainerAllowlist(t *testing.T) {
	backend := &fakeBackend{}
	handler := newTestHandler(t, backend)
	body := `{"container":"palworld-server"}`

	if got := brokerRequest(handler, "wrong", "/v1/status", body).Code; got != http.StatusUnauthorized {
		t.Fatalf("wrong-token status = %d", got)
	}
	if got := brokerRequest(handler, testToken, "/v1/status", `{"container":"other"}`).Code; got != http.StatusForbidden {
		t.Fatalf("disallowed-container status = %d", got)
	}
	if got := brokerRequest(handler, testToken, "/v1/exec", body).Code; got != http.StatusNotFound {
		t.Fatalf("arbitrary-operation status = %d", got)
	}
	if backend.action != "" {
		t.Fatalf("backend was unexpectedly invoked: %s", backend.action)
	}
}

func TestServerExposesOnlyFixedOperations(t *testing.T) {
	for _, action := range []string{"status", "logs", "start", "stop", "backup"} {
		t.Run(action, func(t *testing.T) {
			backend := &fakeBackend{}
			response := brokerRequest(
				newTestHandler(t, backend),
				testToken,
				"/v1/"+action,
				`{"container":"palworld-server"}`,
			)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", response.Code, response.Body)
			}
			if backend.action != action || backend.container != "palworld-server" {
				t.Fatalf("backend call = %s(%q)", backend.action, backend.container)
			}
		})
	}
}

func TestServerRejectsMalformedAndOversizedRequests(t *testing.T) {
	handler := newTestHandler(t, &fakeBackend{})
	if got := brokerRequest(handler, testToken, "/v1/status", `{"container":"palworld-server","command":"id"}`).Code; got != http.StatusBadRequest {
		t.Fatalf("unknown-field status = %d", got)
	}
	oversized := bytes.Repeat([]byte("x"), maxRequestBytes+1)
	if got := brokerRequest(handler, testToken, "/v1/status", string(oversized)).Code; got != http.StatusBadRequest {
		t.Fatalf("oversized status = %d", got)
	}
}
