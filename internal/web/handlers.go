// Package web serves the control website: status page, captcha flows, the
// public players/health JSON endpoints and the boot progress page.
package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arumes31/palworld-starter/internal/captcha"
	"arumes31/palworld-starter/internal/discord"
	"arumes31/palworld-starter/internal/game"
	"arumes31/palworld-starter/internal/state"
)

// BootEstimateSeconds is the typical time the Palworld server needs from
// container start until its REST API answers (i.e. the server is joinable).
const BootEstimateSeconds = 90

// Server holds the web layer's dependencies.
type Server struct {
	game          *game.Controller
	state         *state.State
	templateDir   string
	staticDir     string
	displayName   string
	serverAddress string
}

// New creates the web server. serverAddress (the game address players
// connect to) comes from SERVER_ADDRESS.
func New(g *game.Controller, st *state.State, templateDir, staticDir string) *Server {
	displayName := os.Getenv("DOCKER_CONTAINER_NAME")
	if displayName == "" {
		displayName = "Palworld Server"
	}
	serverAddress := os.Getenv("SERVER_ADDRESS")
	if serverAddress == "" {
		serverAddress = "80.66.59.216:8211"
	}
	return &Server{
		game:          g,
		state:         st,
		templateDir:   templateDir,
		staticDir:     staticDir,
		displayName:   displayName,
		serverAddress: serverAddress,
	}
}

// Routes returns the HTTP mux with all handlers registered.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/captcha", s.handleCaptchaPage(false))
	mux.HandleFunc("/captcha_start", s.handleCaptchaPage(true))
	mux.HandleFunc("/captcha_error", s.handleCaptchaError)
	mux.HandleFunc("/captcha/num", s.handleCaptchaImage)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/add_time", s.handleAddTime)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/starting", s.handleStarting)
	mux.HandleFunc("/api/players", s.handlePlayers)
	mux.HandleFunc("/healthz", s.handleHealthz)

	fileServer := http.FileServer(http.Dir(s.staticDir))
	mux.Handle("/static/", http.StripPrefix("/static", fileServer))
	return mux
}

// PageContext is the data passed to every template.
type PageContext struct {
	Language            string
	DockerContainerName string
	Status              string
	TimeRemaining       int
	DiscordUrl          string
	QuestionSegments    []captcha.Segment
	RetryTarget         string
	CsrfToken           string
	ServerAddress       string
	BootEstimateSeconds int
}

func (s *Server) renderTemplate(w http.ResponseWriter, tmplName string, ctx PageContext) {
	if ctx.Language == "" {
		ctx.Language = "en"
	}

	t, err := template.New("").Funcs(template.FuncMap{
		"title": func(s string) string {
			if s == "" {
				return ""
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"tojson": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"range1000": func() []int {
			res := make([]int, 1000)
			for i := 0; i < 1000; i++ {
				res[i] = i
			}
			return res
		},
	}).ParseFiles(
		filepath.Join(s.templateDir, "base.html"),
		filepath.Join(s.templateDir, tmplName),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Template error: %v", err), http.StatusInternalServerError)
		return
	}

	err = t.ExecuteTemplate(w, "base.html", ctx)
	if err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	sessionData := getSession(r)
	status := s.game.CachedStatus()
	discordUrl := discord.InviteURL()
	remaining := s.state.GetTimeRemaining()

	s.renderTemplate(w, "index.html", PageContext{
		Language:            sessionData.Language,
		DockerContainerName: s.displayName,
		Status:              status,
		TimeRemaining:       remaining,
		DiscordUrl:          discordUrl,
		ServerAddress:       s.serverAddress,
	})
}

func (s *Server) handleCaptchaPage(isStart bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionData := getSession(r)

		lang := r.URL.Query().Get("lang")
		if lang == "" {
			lang = sessionData.Language
		}
		if lang != "de" && lang != "en" {
			lang = "en"
		}
		sessionData.Language = lang

		// Generate captcha; the numbers live only in the encrypted session
		// and are served as scratch-off images, never as text.
		ch := captcha.Generate(lang)
		sessionData.CaptchaAnswer = ch.Answer
		sessionData.CaptchaNum1 = ch.Num1
		sessionData.CaptchaNum2 = ch.Num2

		// Ensure CSRF token exists
		if sessionData.CsrfToken == "" {
			tokenBytes := make([]byte, 16)
			_, _ = rand.Read(tokenBytes)
			sessionData.CsrfToken = hex.EncodeToString(tokenBytes)
		}
		saveSession(w, sessionData)

		discordUrl := discord.InviteURL()
		tmplName := "captcha.html"
		if isStart {
			tmplName = "captcha_start.html"
		}

		s.renderTemplate(w, tmplName, PageContext{
			Language:         lang,
			QuestionSegments: ch.Segments(),
			DiscordUrl:       discordUrl,
			CsrfToken:        sessionData.CsrfToken,
		})
	}
}

// handleCaptchaImage serves the session's captcha numbers as PNGs
// (?i=1 or ?i=2). The visitor scratches a canvas overlay free to read them.
func (s *Server) handleCaptchaImage(w http.ResponseWriter, r *http.Request) {
	sessionData := getSession(r)

	var n int
	switch r.URL.Query().Get("i") {
	case "1":
		n = sessionData.CaptchaNum1
	case "2":
		n = sessionData.CaptchaNum2
	}
	if n <= 0 {
		http.NotFound(w, r)
		return
	}

	pngBytes, err := captcha.RenderNumberPNG(n)
	if err != nil {
		http.Error(w, "image error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pngBytes)
}

func (s *Server) handleCaptchaError(w http.ResponseWriter, r *http.Request) {
	sessionData := getSession(r)
	origin := r.URL.Query().Get("origin")
	if origin == "" {
		origin = "add_time"
	}
	discordUrl := discord.InviteURL()

	s.renderTemplate(w, "captcha_error.html", PageContext{
		Language:    sessionData.Language,
		DiscordUrl:  discordUrl,
		RetryTarget: origin,
	})
}

// clearCaptcha invalidates the solved captcha so it cannot be replayed.
func clearCaptcha(sessionData *SessionData) {
	sessionData.CaptchaAnswer = -9999
	sessionData.CaptchaNum1 = 0
	sessionData.CaptchaNum2 = 0
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionData := getSession(r)
	submittedCsrf := r.FormValue("csrf_token")
	if submittedCsrf == "" || submittedCsrf != sessionData.CsrfToken {
		http.Error(w, "Forbidden - CSRF verification failed", http.StatusForbidden)
		return
	}

	ansStr := r.FormValue("captcha_answer")
	ans, err := strconv.Atoi(ansStr)
	if err != nil || ans != sessionData.CaptchaAnswer {
		http.Redirect(w, r, "/captcha_error?origin=start_container", http.StatusSeeOther)
		return
	}

	clearCaptcha(sessionData)
	saveSession(w, sessionData)

	// Start container if not running
	status := s.game.Status()
	if status != "running" {
		if err := s.game.Start(); err != nil {
			log.Printf("Failed to start container: %v", err)
		} else {
			s.state.UpdateTimeRemaining(func(current int) int {
				val := current
				if val < 900 {
					val = 900
				}
				return val + 14400
			})
			log.Println("Server started + 4 hours added")
			// Show boot progress until the game's REST API is reachable.
			http.Redirect(w, r, "/starting", http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleAddTime(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionData := getSession(r)
	submittedCsrf := r.FormValue("csrf_token")
	if submittedCsrf == "" || submittedCsrf != sessionData.CsrfToken {
		http.Error(w, "Forbidden - CSRF verification failed", http.StatusForbidden)
		return
	}

	ansStr := r.FormValue("captcha_answer")
	ans, err := strconv.Atoi(ansStr)
	if err != nil || ans != sessionData.CaptchaAnswer {
		http.Redirect(w, r, "/captcha_error?origin=add_time", http.StatusSeeOther)
		return
	}

	clearCaptcha(sessionData)
	saveSession(w, sessionData)

	status := s.game.Status()
	if status == "running" {
		remaining := s.state.UpdateTimeRemaining(func(current int) int {
			return current + 43200
		})
		log.Printf("+12 hours added, now %dh", remaining/3600)
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Restrict to localhost / loopback
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	status := s.game.Status()
	if status == "running" {
		if err := s.game.Stop(); err != nil {
			log.Printf("Stop failed: %v", err)
		} else {
			log.Println("Container stopped due to time expiry or manual stop")
		}
	}
	s.state.SetTimeRemaining(0)

	_, _ = w.Write([]byte("OK"))
}

// handleStarting shows a boot progress page that polls /api/players until
// the game's REST API is reachable ("joinable"), then returns to the index.
func (s *Server) handleStarting(w http.ResponseWriter, r *http.Request) {
	sessionData := getSession(r)

	s.renderTemplate(w, "starting.html", PageContext{
		Language:            sessionData.Language,
		DockerContainerName: s.displayName,
		Status:              s.game.CachedStatus(),
		DiscordUrl:          discord.InviteURL(),
		ServerAddress:       s.serverAddress,
		BootEstimateSeconds: BootEstimateSeconds,
	})
}

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	players := s.game.Players()
	if players == nil {
		players = []game.PlayerInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   s.game.CachedStatus(),
		"count":    len(players),
		"players":  players,
		"joinable": s.game.RestAPIUp(),
	})
}

// handleHealthz is a liveness endpoint for uptime monitoring. It always
// answers 200 when the web process is up and reports the container status.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "ok",
		"container":      s.game.CachedStatus(),
		"time_remaining": s.state.GetTimeRemaining(),
	})
}
