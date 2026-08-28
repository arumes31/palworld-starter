// Package broker exposes a deliberately narrow, authenticated control surface
// in front of the Docker socket.
package broker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const maxRequestBytes = 4096

var ErrNotFound = errors.New("container not found")

type Backend interface {
	Status(context.Context, string) (string, error)
	Logs(context.Context, string) (string, error)
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Backup(context.Context, string) (int, string, error)
}

type Server struct {
	backend Backend
	token   string
	allowed map[string]struct{}
}

type controlRequest struct {
	Container string `json:"container"`
}

type controlResponse struct {
	Status   string `json:"status,omitempty"`
	Logs     string `json:"logs,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Output   string `json:"output,omitempty"`
}

func NewServer(backend Backend, token string, allowedContainers []string) (*Server, error) {
	if backend == nil {
		return nil, fmt.Errorf("backend is required")
	}
	if len(token) < 32 {
		return nil, fmt.Errorf("BROKER_TOKEN must contain at least 32 characters")
	}
	allowed := make(map[string]struct{}, len(allowedContainers))
	for _, name := range allowedContainers {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("at least one allowlisted container is required")
	}
	return &Server{backend: backend, token: token, allowed: allowed}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	for _, action := range []string{"status", "logs", "start", "stop", "backup"} {
		mux.HandleFunc("POST /v1/"+action, s.handle(action))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	})
	return mux
}

func (s *Server) handle(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(s.token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request controlRequest
		if err := decoder.Decode(&request); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if _, ok := s.allowed[request.Container]; !ok {
			http.Error(w, "container not allowed", http.StatusForbidden)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
		defer cancel()
		var result controlResponse
		var err error
		switch action {
		case "status":
			result.Status, err = s.backend.Status(ctx, request.Container)
		case "logs":
			result.Logs, err = s.backend.Logs(ctx, request.Container)
		case "start":
			err = s.backend.Start(ctx, request.Container)
		case "stop":
			err = s.backend.Stop(ctx, request.Container)
		case "backup":
			result.ExitCode, result.Output, err = s.backend.Backup(ctx, request.Container)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "container not found", http.StatusNotFound)
				return
			}
			log.Printf("broker %s failed for %q: %v", action, request.Container, err)
			http.Error(w, "container operation failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("broker response encoding failed: %v", err)
		}
	}
}
