package web

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/arumes31/palworld-starter/internal/admin"
	"github.com/arumes31/palworld-starter/internal/discord"
	"github.com/arumes31/palworld-starter/internal/game"
)

// adminSessionTTL is how long an admin login stays valid.
const adminSessionTTL = 8 * time.Hour

// adminServerView is the per-server view model for the admin dashboard.
type adminServerView struct {
	ID               string
	Name             string
	Address          string
	Status           string
	Joinable         bool
	TimeRemaining    int
	HasPassword      bool
	RebootActive     bool
	RebootTargetUnix int64
	Jobs             []adminJobView
}

// adminJobView is one scheduled reboot as shown in the dashboard.
type adminJobView struct {
	ID          string
	ServerID    string
	Type        string
	Time        string
	LeadSeconds int
	Enabled     bool
	NextRun     string
}

// registerAdminRoutes wires the admin GUI when it is enabled.
func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	if s.admin == nil || !s.admin.Enabled() {
		return
	}
	mux.HandleFunc("/admin", s.handleAdminDashboard)
	mux.HandleFunc("/admin/login", s.handleAdminLogin)
	mux.HandleFunc("/admin/logout", s.handleAdminLogout)
	mux.HandleFunc("/admin/action", s.handleAdminAction)
	mux.HandleFunc("/admin/schedule", s.handleAdminSchedule)
	mux.HandleFunc("/admin/password", s.handleAdminPassword)
	mux.HandleFunc("/admin/api/players", s.handleAdminPlayers)
}

// rebootStatus reports the target time of an in-progress reboot for a server,
// if any. Safe to call from the public pages: it tolerates a nil manager and
// works whether or not the admin GUI itself is enabled.
func (s *Server) rebootStatus(serverID string) (targetUnix int64, active bool) {
	if s.admin == nil {
		return 0, false
	}
	t, ok := s.admin.ActiveReboot(serverID)
	if !ok {
		return 0, false
	}
	return t.Unix(), true
}

// adminScope returns the caller's admin scope, or "" when not authenticated.
func (s *Server) adminScope(sd *SessionData) string {
	if sd.AdminScope == "" || sd.AdminExpires < time.Now().Unix() {
		return ""
	}
	return sd.AdminScope
}

// ensureCsrf makes sure the session carries a CSRF token, minting one if not.
func ensureCsrf(sd *SessionData) {
	if sd.CsrfToken == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		sd.CsrfToken = hex.EncodeToString(b)
	}
}

// checkCsrf validates the submitted CSRF token against the session.
func checkCsrf(sd *SessionData, r *http.Request) bool {
	tok := r.FormValue("csrf_token")
	return tok != "" && tok == sd.CsrfToken
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	sd := getSession(r)

	if r.Method == http.MethodGet {
		ensureCsrf(sd)
		saveSession(w, sd)
		s.renderTemplate(w, r, "admin_login.html", PageContext{
			Language:   sd.Language,
			CsrfToken:  sd.CsrfToken,
			DiscordUrl: discord.InviteURL(),
			AdminError: r.URL.Query().Get("err"),
		})
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if !checkCsrf(sd, r) {
		http.Error(w, "Forbidden - CSRF verification failed", http.StatusForbidden)
		return
	}

	scope, ok := s.admin.Authenticate(r.FormValue("password"))
	if !ok {
		// Slow down brute-force attempts without holding a lock or state.
		time.Sleep(600 * time.Millisecond)
		http.Redirect(w, r, "/admin/login?err=invalid", http.StatusSeeOther)
		return
	}

	sd.AdminScope = scope
	sd.AdminExpires = time.Now().Add(adminSessionTTL).Unix()
	ensureCsrf(sd)
	saveSession(w, sd)
	log.Printf("admin: login for scope %q", scope)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	sd := getSession(r)
	sd.AdminScope = ""
	sd.AdminExpires = 0
	saveSession(w, sd)
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	sd := getSession(r)
	scope := s.adminScope(sd)
	if scope == "" {
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
		return
	}
	ensureCsrf(sd)
	saveSession(w, sd)

	jobsByServer := map[string][]adminJobView{}
	now := time.Now()
	for _, j := range s.admin.Jobs(scope) {
		jobsByServer[j.ServerID] = append(jobsByServer[j.ServerID], adminJobView{
			ID:          j.ID,
			ServerID:    j.ServerID,
			Type:        string(j.Type),
			Time:        j.Time,
			LeadSeconds: j.LeadSeconds,
			Enabled:     j.Enabled,
			NextRun:     nextRunLabel(j, now),
		})
	}

	var views []adminServerView
	for _, ref := range s.admin.VisibleServers(scope) {
		status := ref.Ctrl.CachedStatus()
		joinable := ref.Ctrl.RestAPIUp()
		if status == "running" && !ref.Ctrl.IsPaused() && !joinable {
			status = "starting"
		}
		target, active := s.admin.ActiveReboot(ref.ID)
		var targetUnix int64
		if active {
			targetUnix = target.Unix()
		}
		views = append(views, adminServerView{
			ID:               ref.ID,
			Name:             ref.Name,
			Address:          ref.Address,
			Status:           status,
			Joinable:         joinable,
			TimeRemaining:    ref.State.GetTimeRemaining(),
			HasPassword:      s.admin.HasServerPassword(ref.ID),
			RebootActive:     active,
			RebootTargetUnix: targetUnix,
			Jobs:             jobsByServer[ref.ID],
		})
	}

	s.renderTemplate(w, r, "admin.html", PageContext{
		Language:     sd.Language,
		CsrfToken:    sd.CsrfToken,
		DiscordUrl:   discord.InviteURL(),
		AdminScope:   scope,
		AdminGlobal:  admin.IsGlobal(scope),
		AdminServers: views,
		AdminError:   r.URL.Query().Get("err"),
		AdminNotice:  r.URL.Query().Get("msg"),
	})
}

// authorizeAction resolves the admin scope and verifies POST + CSRF + access to
// the target server. It returns the scope and target server id, or writes the
// error response and returns ok=false.
func (s *Server) authorizeAction(w http.ResponseWriter, r *http.Request) (scope, serverID string, ok bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return "", "", false
	}
	sd := getSession(r)
	scope = s.adminScope(sd)
	if scope == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", "", false
	}
	if !checkCsrf(sd, r) {
		http.Error(w, "Forbidden - CSRF verification failed", http.StatusForbidden)
		return "", "", false
	}
	serverID = r.FormValue("srv")
	if serverID != "" && !admin.CanAccess(scope, serverID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return "", "", false
	}
	return scope, serverID, true
}

func (s *Server) handleAdminAction(w http.ResponseWriter, r *http.Request) {
	scope, serverID, ok := s.authorizeAction(w, r)
	if !ok {
		return
	}
	ref, exists := s.admin.Server(serverID)
	if !exists {
		s.adminRedirect(w, r, "", "unknown server")
		return
	}

	action := r.FormValue("action")
	var err error
	msg := ""
	switch action {
	case "announce":
		text := strings.TrimSpace(r.FormValue("message"))
		if text == "" {
			err = errEmpty("message")
		} else {
			err = ref.Ctrl.AnnounceNow(text)
			msg = "Announcement sent"
		}
	case "kick":
		err = ref.Ctrl.Kick(r.FormValue("userid"), r.FormValue("message"))
		msg = "Player kicked"
	case "ban":
		err = ref.Ctrl.Ban(r.FormValue("userid"), r.FormValue("message"))
		msg = "Player banned"
	case "unban":
		err = ref.Ctrl.Unban(strings.TrimSpace(r.FormValue("userid")))
		msg = "Player unbanned"
	case "save":
		err = ref.Ctrl.SaveWorld()
		msg = "World saved"
	case "start":
		err = ref.Ctrl.Start()
		if err == nil {
			ref.State.UpdateTimeRemaining(func(cur int) int {
				if cur < 43200 {
					return 43200
				}
				return cur
			})
		}
		msg = "Server starting"
	case "stop":
		err = ref.Ctrl.Stop()
		ref.State.SetTimeRemaining(0)
		msg = "Server stopped"
	case "reboot":
		lead := parseLead(r.FormValue("lead"))
		err = s.admin.RebootNow(serverID, lead, "admin "+scope)
		msg = "Reboot scheduled"
	case "cancel_reboot":
		if s.admin.CancelReboot(serverID) {
			msg = "Reboot cancelled"
		} else {
			err = errPlain("no reboot in progress")
		}
	default:
		err = errPlain("unknown action")
	}

	if err != nil {
		s.adminRedirect(w, r, "", err.Error())
		return
	}
	s.adminRedirect(w, r, msg, "")
}

func (s *Server) handleAdminSchedule(w http.ResponseWriter, r *http.Request) {
	scope, serverID, ok := s.authorizeAction(w, r)
	if !ok {
		return
	}

	switch r.FormValue("op") {
	case "add":
		job := &admin.RebootJob{
			ServerID:    serverID,
			Type:        admin.JobType(r.FormValue("type")),
			Time:        strings.TrimSpace(r.FormValue("time")),
			LeadSeconds: parseLead(r.FormValue("lead")),
		}
		if _, err := s.admin.AddJob(job); err != nil {
			s.adminRedirect(w, r, "", err.Error())
			return
		}
		s.adminRedirect(w, r, "Schedule added", "")
	case "delete":
		if !s.admin.DeleteJob(scope, r.FormValue("id")) {
			s.adminRedirect(w, r, "", "schedule not found")
			return
		}
		s.adminRedirect(w, r, "Schedule removed", "")
	case "toggle":
		if !s.admin.ToggleJob(scope, r.FormValue("id")) {
			s.adminRedirect(w, r, "", "schedule not found")
			return
		}
		s.adminRedirect(w, r, "Schedule updated", "")
	default:
		s.adminRedirect(w, r, "", "unknown schedule operation")
	}
}

// handleAdminPassword sets or clears a per-server admin password. Only global
// admins may manage per-server passwords.
func (s *Server) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	scope, serverID, ok := s.authorizeAction(w, r)
	if !ok {
		return
	}
	if !admin.IsGlobal(scope) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if err := s.admin.SetServerPassword(serverID, r.FormValue("password")); err != nil {
		s.adminRedirect(w, r, "", err.Error())
		return
	}
	if r.FormValue("password") == "" {
		s.adminRedirect(w, r, "Server password cleared", "")
	} else {
		s.adminRedirect(w, r, "Server password set", "")
	}
}

func (s *Server) handleAdminPlayers(w http.ResponseWriter, r *http.Request) {
	sd := getSession(r)
	scope := s.adminScope(sd)
	if scope == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	serverID := r.URL.Query().Get("srv")
	if !admin.CanAccess(scope, serverID) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	ref, ok := s.admin.Server(serverID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	players, up := ref.Ctrl.AdminPlayers()
	if players == nil {
		players = []game.AdminPlayerInfo{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"server":   serverID,
		"status":   ref.Ctrl.CachedStatus(),
		"joinable": up,
		"count":    len(players),
		"players":  players,
	})
}

// adminRedirect sends the admin back to the dashboard with a notice or error.
func (s *Server) adminRedirect(w http.ResponseWriter, r *http.Request, msg, errMsg string) {
	target := "/admin"
	if errMsg != "" {
		target += "?err=" + url.QueryEscape(errMsg)
	} else if msg != "" {
		target += "?msg=" + url.QueryEscape(msg)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func errPlain(msg string) error   { return fmt.Errorf("%s", msg) }
func errEmpty(field string) error { return fmt.Errorf("%s is required", field) }

func parseLead(raw string) int {
	if raw == "" {
		return admin.DefaultLeadSeconds
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return admin.DefaultLeadSeconds
	}
	return n
}

func nextRunLabel(j *admin.RebootJob, now time.Time) string {
	t, err := admin.NextTarget(j, now)
	if err != nil {
		return "invalid"
	}
	return t.Format("2006-01-02 15:04")
}
