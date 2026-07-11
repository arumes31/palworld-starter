// Package web serves the control website: status page, captcha flows, the
// public players/health JSON endpoints and the boot progress page. It can
// manage any number of Palworld servers; handlers select the target server
// via the "srv" query/form parameter and default to the first one.
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
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"arumes31/palworld-starter/internal/captcha"
	"arumes31/palworld-starter/internal/discord"
	"arumes31/palworld-starter/internal/game"
	"arumes31/palworld-starter/internal/state"
)

// BootEstimateSeconds is the typical time the Palworld server needs from
// container start until its REST API answers (i.e. the server is joinable).
const BootEstimateSeconds = 90

// Instance is one managed Palworld server.
type Instance struct {
	ID          string
	DisplayName string
	Address     string // public game address (ip:port)
	Game        *game.Controller
	State       *state.State
}

// Server holds the web layer's dependencies.
type Server struct {
	instances   []*Instance
	byID        map[string]*Instance
	templateDir string
	staticDir   string
}

// New creates the web server for the given server instances (at least one).
func New(instances []*Instance, templateDir, staticDir string) *Server {
	byID := make(map[string]*Instance, len(instances))
	for _, inst := range instances {
		byID[inst.ID] = inst
	}
	return &Server{
		instances:   instances,
		byID:        byID,
		templateDir: templateDir,
		staticDir:   staticDir,
	}
}

// resolveInstance picks the server addressed by the "srv" query or form
// parameter, defaulting to the first configured server.
func (s *Server) resolveInstance(r *http.Request) *Instance {
	id := r.URL.Query().Get("srv")
	if id == "" {
		id = r.FormValue("srv")
	}
	if inst, ok := s.byID[id]; ok {
		return inst
	}
	return s.instances[0]
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

// ServerPanel is the per-server view model for the index page.
type ServerPanel struct {
	ID            string
	DisplayName   string
	Address       string
	Status        string
	TimeRemaining int
}

// PageContext is the data passed to every template.
type PageContext struct {
	Language            string
	DockerContainerName string
	ServerID            string
	Status              string
	TimeRemaining       int
	DiscordUrl          string
	QuestionSegments    []captcha.Segment
	RetryTarget         string
	CsrfToken           string
	ServerAddress       string
	BootEstimateSeconds int
	Servers             []ServerPanel
	AppVersion          string
}

var AppVersion = strconv.FormatInt(time.Now().Unix(), 10)

func (s *Server) renderTemplate(w http.ResponseWriter, tmplName string, ctx PageContext) {
	if ctx.Language == "" {
		ctx.Language = "de"
	}

	ctx.AppVersion = AppVersion
	t, err := template.New("base.html").Funcs(template.FuncMap{
		"title": func(s string) string {
			if s == "" {
				return ""
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"tojson": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b) // #nosec G203 //nolint:gosec // marshaled from typed server-side data, never raw user input
		},
		"range1000": func() []int {
			res := make([]int, 1000)
			for i := 0; i < 1000; i++ {
				res[i] = i
			}
			return res
		},
		"mod": func(i, j int) int {
			return i % j
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
	// Honor the footer language toggle and remember the choice.
	if lang := r.URL.Query().Get("lang"); lang == "de" || lang == "en" {
		sessionData.Language = lang
		saveSession(w, sessionData)
	}

	panels := make([]ServerPanel, 0, len(s.instances))
	for _, inst := range s.instances {
		panels = append(panels, ServerPanel{
			ID:            inst.ID,
			DisplayName:   inst.DisplayName,
			Address:       inst.Address,
			Status:        inst.Game.CachedStatus(),
			TimeRemaining: inst.State.GetTimeRemaining(),
		})
	}

	s.renderTemplate(w, "index.html", PageContext{
		Language:   sessionData.Language,
		DiscordUrl: discord.InviteURL(),
		Servers:    panels,
	})
}

func (s *Server) handleCaptchaPage(isStart bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionData := getSession(r)
		inst := s.resolveInstance(r)

		lang := r.URL.Query().Get("lang")
		if lang == "" {
			lang = sessionData.Language
		}
		if lang != "de" && lang != "en" {
			lang = "en"
		}
		sessionData.Language = lang

		// Generate captcha; the numbers live only in the encrypted session
		// and are served as scratch-off images, never as text. Retry a few
		// times so the visitor never sees the same story twice in a row.
		ch := captcha.Generate(lang)
		for tries := 0; tries < 5 && ch.Fingerprint == sessionData.LastCaptcha; tries++ {
			ch = captcha.Generate(lang)
		}
		sessionData.LastCaptcha = ch.Fingerprint
		sessionData.CaptchaAnswer = ch.Answer
		sessionData.CaptchaNum1 = ch.Num1
		sessionData.CaptchaNum2 = ch.Num2
		sessionData.CaptchaServer = inst.ID

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
			Language:            lang,
			DockerContainerName: inst.DisplayName,
			ServerID:            inst.ID,
			QuestionSegments:    ch.Segments(),
			DiscordUrl:          discordUrl,
			CsrfToken:           sessionData.CsrfToken,
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
		ServerID:    s.resolveInstance(r).ID,
		DiscordUrl:  discordUrl,
		RetryTarget: origin,
	})
}

// captchaInstance returns the server the visitor's captcha was issued for.
func (s *Server) captchaInstance(sessionData *SessionData) *Instance {
	if inst, ok := s.byID[sessionData.CaptchaServer]; ok {
		return inst
	}
	return s.instances[0]
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

	inst := s.captchaInstance(sessionData)

	ansStr := r.FormValue("captcha_answer")
	ans, err := strconv.Atoi(ansStr)
	if err != nil || ans != sessionData.CaptchaAnswer {
		http.Redirect(w, r, "/captcha_error?origin=start_container&srv="+inst.ID, http.StatusSeeOther)
		return
	}

	clearCaptcha(sessionData)
	saveSession(w, sessionData)

	// Start container if not running
	status := inst.Game.Status()
	if status != "running" {
		if err := inst.Game.Start(); err != nil {
			log.Printf("Failed to start container %s: %v", inst.ID, err)
		} else {
			inst.State.UpdateTimeRemaining(func(current int) int {
				val := current
				if val < 900 {
					val = 900
				}
				return val + 43200
			})
			log.Printf("Server %s started + 12 hours added", inst.ID)
			// Show boot progress until the game's REST API is reachable.
			http.Redirect(w, r, "/starting?srv="+inst.ID, http.StatusSeeOther)
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

	inst := s.captchaInstance(sessionData)

	ansStr := r.FormValue("captcha_answer")
	ans, err := strconv.Atoi(ansStr)
	if err != nil || ans != sessionData.CaptchaAnswer {
		http.Redirect(w, r, "/captcha_error?origin=add_time&srv="+inst.ID, http.StatusSeeOther)
		return
	}

	clearCaptcha(sessionData)
	saveSession(w, sessionData)

	status := inst.Game.Status()
	if status == "running" {
		remaining := inst.State.UpdateTimeRemaining(func(current int) int {
			return current + 43200
		})
		log.Printf("+12 hours added to %s, now %dh", inst.ID, remaining/3600)
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

	inst := s.resolveInstance(r)
	status := inst.Game.Status()
	if status == "running" {
		if err := inst.Game.Stop(); err != nil {
			log.Printf("Stop failed for %s: %v", inst.ID, err)
		} else {
			log.Printf("Container %s stopped due to time expiry or manual stop", inst.ID)
		}
	}
	inst.State.SetTimeRemaining(0)

	_, _ = w.Write([]byte("OK"))
}

// handleStarting shows a boot progress page that polls /api/players until
// the game's REST API is reachable ("joinable"), then returns to the index.
func (s *Server) handleStarting(w http.ResponseWriter, r *http.Request) {
	sessionData := getSession(r)
	inst := s.resolveInstance(r)

	s.renderTemplate(w, "starting.html", PageContext{
		Language:            sessionData.Language,
		DockerContainerName: inst.DisplayName,
		ServerID:            inst.ID,
		Status:              inst.Game.CachedStatus(),
		DiscordUrl:          discord.InviteURL(),
		ServerAddress:       inst.Address,
		BootEstimateSeconds: BootEstimateSeconds,
	})
}

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	inst := s.resolveInstance(r)
	players := inst.Game.Players()
	if players == nil {
		players = []game.PlayerInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"server":   inst.ID,
		"status":   inst.Game.CachedStatus(),
		"count":    len(players),
		"players":  players,
		"joinable": inst.Game.RestAPIUp(),
	})
}

// handleHealthz is a liveness endpoint for uptime monitoring. It always
// answers 200 when the web process is up and reports every container status.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	servers := make([]map[string]interface{}, 0, len(s.instances))
	for _, inst := range s.instances {
		servers = append(servers, map[string]interface{}{
			"id":             inst.ID,
			"container":      inst.Game.CachedStatus(),
			"time_remaining": inst.State.GetTimeRemaining(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"servers": servers,
	})
}
