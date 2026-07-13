// Package web serves the control website: status page, captcha flows, the
// public players/health JSON endpoints and the boot progress page. It can
// manage any number of Palworld servers; handlers select the target server
// via the "srv" query/form parameter and default to the first one.
package web

import (
	"crypto/rand"
	"crypto/subtle"
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
	"time"

	"github.com/arumes31/palworld-starter/internal/captcha"
	"github.com/arumes31/palworld-starter/internal/discord"
	"github.com/arumes31/palworld-starter/internal/game"
	"github.com/arumes31/palworld-starter/internal/state"
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
	instances []*Instance
	byID      map[string]*Instance
	staticDir string
	templates map[string]*template.Template
	stopToken string
}

// New creates the web server for the given server instances (at least one).
// All templates are parsed once here; a broken template fails the process at
// startup instead of 500-ing every request later.
func New(instances []*Instance, templateDir, staticDir string) *Server {
	byID := make(map[string]*Instance, len(instances))
	for _, inst := range instances {
		byID[inst.ID] = inst
	}
	return &Server{
		instances: instances,
		byID:      byID,
		staticDir: staticDir,
		templates: parseTemplates(templateDir),
		stopToken: os.Getenv("STOP_TOKEN"),
	}
}

var templateFuncs = template.FuncMap{
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
}

// parseTemplates parses every page template together with base.html.
func parseTemplates(templateDir string) map[string]*template.Template {
	entries, err := os.ReadDir(templateDir)
	if err != nil {
		log.Fatalf("Cannot read template dir %s: %v", templateDir, err)
	}

	templates := make(map[string]*template.Template)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == "base.html" || !strings.HasSuffix(name, ".html") {
			continue
		}
		t, err := template.New("base.html").Funcs(templateFuncs).ParseFiles(
			filepath.Join(templateDir, "base.html"),
			filepath.Join(templateDir, name),
		)
		if err != nil {
			log.Fatalf("Template %s: %v", name, err)
		}
		templates[name] = t
	}
	return templates
}

// resolveInstance picks the server addressed by the "srv" query or form
// parameter. An empty parameter selects the first configured server; an
// unknown id returns nil and the caller must answer 404, so a mistyped id
// can never target the wrong server.
func (s *Server) resolveInstance(r *http.Request) *Instance {
	id := r.URL.Query().Get("srv")
	if id == "" {
		id = r.FormValue("srv")
	}
	if id == "" {
		return s.instances[0]
	}
	return s.byID[id]
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
	mux.HandleFunc("/robots.txt", s.handleRobots)
	mux.HandleFunc("/sitemap.xml", s.handleSitemap)
	mux.HandleFunc("/terms", s.handleTerms)
	mux.HandleFunc("/privacy", s.handlePrivacy)

	fileServer := http.FileServer(http.Dir(s.staticDir))
	mux.Handle("/static/", http.StripPrefix("/static", fileServer))
	return mux
}

// ServerPanel is the per-server view model for the index page. Status adds a
// synthetic "starting" over the raw container states: the container runs but
// the game's REST API is not up yet, i.e. the server is booting and not
// joinable.
type ServerPanel struct {
	ID            string
	DisplayName   string
	Address       string
	Status        string
	TimeRemaining int
	Metrics       game.ServerMetrics
	GameVersion   string
	GameMode      string // "pvp", "pve" or "" while unknown
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
	Servers                []ServerPanel
	AppVersion             string
	SiteURL                string
	GoogleSiteVerification string
	BingSiteVerification   string
	YandexSiteVerification string
}

// siteURL returns the public website base URL, always with a trailing slash.
func (s *Server) siteURL(r *http.Request) string {
	if u := os.Getenv("WEBSITE_URL"); u != "" {
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return u
	}
	if r == nil {
		u := game.WebsiteURL()
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return u
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		host = xfh
	}
	return scheme + "://" + host + "/"
}

var AppVersion = strconv.FormatInt(time.Now().Unix(), 10)

func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, tmplName string, ctx PageContext) {
	if ctx.Language == "" {
		ctx.Language = "de"
	}

	ctx.AppVersion = AppVersion
	ctx.SiteURL = s.siteURL(r)
	ctx.GoogleSiteVerification = os.Getenv("GOOGLE_SITE_VERIFICATION")
	ctx.BingSiteVerification = os.Getenv("BING_SITE_VERIFICATION")
	ctx.YandexSiteVerification = os.Getenv("YANDEX_SITE_VERIFICATION")
	t, ok := s.templates[tmplName]
	if !ok {
		http.Error(w, "Unknown template: "+tmplName, http.StatusInternalServerError)
		return
	}

	if err := t.ExecuteTemplate(w, "base.html", ctx); err != nil {
		log.Printf("Template execution error: %v", err)
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// Support search engine verification files and favicons at root level
		cleanPath := filepath.Clean(r.URL.Path)
		if !strings.Contains(cleanPath, "..") {
			name := filepath.Base(cleanPath)
			// Check if it's a known verification pattern or icon
			isAllowed := (strings.HasPrefix(name, "google") && strings.HasSuffix(name, ".html")) ||
				(strings.HasPrefix(name, "yandex_") && strings.HasSuffix(name, ".html")) ||
				(strings.HasPrefix(name, "pinterest-") && strings.HasSuffix(name, ".html")) ||
				name == "BingSiteAuth.xml" ||
				name == "favicon.ico" ||
				name == "apple-touch-icon.png" ||
				name == "apple-touch-icon-precomposed.png"

			if isAllowed {
				fullPath := filepath.Join(s.staticDir, name)
				if _, err := os.Stat(fullPath); err == nil {
					http.ServeFile(w, r, fullPath)
					return
				}
			}
		}
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
		status := inst.Game.CachedStatus()
		// Booting: container up, but the game is not joinable yet. Auto-paused
		// servers stay "running" - they wake on the first join attempt.
		if status == "running" && !inst.Game.IsPaused() && !inst.Game.RestAPIUp() {
			status = "starting"
		}
		panels = append(panels, ServerPanel{
			ID:            inst.ID,
			DisplayName:   inst.DisplayName,
			Address:       inst.Address,
			Status:        status,
			TimeRemaining: inst.State.GetTimeRemaining(),
			Metrics:       inst.Game.Metrics(),
			GameVersion:   inst.Game.Info().Version,
			GameMode:      inst.Game.GameMode(),
		})
	}

	s.renderTemplate(w, r, "index.html", PageContext{
		Language:   sessionData.Language,
		DiscordUrl: discord.InviteURL(),
		Servers:    panels,
	})
}

func (s *Server) handleCaptchaPage(isStart bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionData := getSession(r)
		inst := s.resolveInstance(r)
		if inst == nil {
			http.NotFound(w, r)
			return
		}

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

		s.renderTemplate(w, r, tmplName, PageContext{
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
	inst := s.resolveInstance(r)
	if inst == nil {
		http.NotFound(w, r)
		return
	}
	origin := r.URL.Query().Get("origin")
	if origin == "" {
		origin = "add_time"
	}
	discordUrl := discord.InviteURL()

	s.renderTemplate(w, r, "captcha_error.html", PageContext{
		Language:    sessionData.Language,
		ServerID:    inst.ID,
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
			msg := "Failed to start the server - please try again in a minute."
			if sessionData.Language == "de" {
				msg = "Server konnte nicht gestartet werden - bitte versuche es in einer Minute erneut."
			}
			http.Error(w, msg, http.StatusBadGateway)
			return
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

	// STOP_TOKEN set: require the shared secret (works behind a reverse
	// proxy where RemoteAddr is useless). Unset: legacy loopback-only check.
	if s.stopToken != "" {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Stop-Token")), []byte(s.stopToken)) != 1 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	} else {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if host != "127.0.0.1" && host != "::1" && host != "localhost" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	inst := s.resolveInstance(r)
	if inst == nil {
		http.NotFound(w, r)
		return
	}
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
	if inst == nil {
		http.NotFound(w, r)
		return
	}

	s.renderTemplate(w, r, "starting.html", PageContext{
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
	if inst == nil {
		http.NotFound(w, r)
		return
	}
	players := inst.Game.Players()
	if players == nil {
		players = []game.PlayerInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	metrics := inst.Game.Metrics()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"server":   inst.ID,
		"status":   inst.Game.CachedStatus(),
		"count":    len(players),
		"players":  players,
		"joinable": inst.Game.RestAPIUp(),
		"fps":      metrics.ServerFPS,
	})
}

// handleRobots invites crawlers to index the status page while keeping them
// out of the captcha flow and the JSON endpoints.
func (s *Server) handleRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, `User-agent: *
Allow: /
Disallow: /captcha
Disallow: /captcha_start
Disallow: /captcha_error
Disallow: /starting
Disallow: /api/

Sitemap: %ssitemap.xml
`, s.siteURL(r))
}

// handleSitemap serves a minimal sitemap: the landing page in both languages.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	base := s.siteURL(r)
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">
  <url>
    <loc>%s</loc>
    <xhtml:link rel="alternate" hreflang="en" href="%s?lang=en"/>
    <xhtml:link rel="alternate" hreflang="de" href="%s?lang=de"/>
    <xhtml:link rel="alternate" hreflang="x-default" href="%s"/>
    <changefreq>hourly</changefreq>
  </url>
</urlset>
`, base, base, base, base)
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

func (s *Server) handleTerms(w http.ResponseWriter, r *http.Request) {
	sessionData := getSession(r)
	inst := s.resolveInstance(r)
	if inst == nil {
		http.NotFound(w, r)
		return
	}

	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = sessionData.Language
	}
	if lang != "de" && lang != "en" {
		lang = "en"
	}

	s.renderTemplate(w, r, "terms.html", PageContext{
		Language:            lang,
		ServerID:            inst.ID,
		DockerContainerName: inst.DisplayName,
	})
}

func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	sessionData := getSession(r)
	inst := s.resolveInstance(r)
	if inst == nil {
		http.NotFound(w, r)
		return
	}

	lang := r.URL.Query().Get("lang")
	if lang == "" {
		lang = sessionData.Language
	}
	if lang != "de" && lang != "en" {
		lang = "en"
	}

	s.renderTemplate(w, r, "privacy.html", PageContext{
		Language:            lang,
		ServerID:            inst.ID,
		DockerContainerName: inst.DisplayName,
	})
}

