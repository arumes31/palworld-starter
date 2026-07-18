// Package admin implements the authenticated admin control layer: per-server
// admin actions (announce/kick/ban/...), scheduled and one-off server reboots
// with in-game countdown announcements, and a two-tier login model.
//
// Access model:
//   - A single global password (ADMIN_GUI_PASSWORD) grants access to every
//     managed server (scope "*").
//   - Each server may additionally have its own password that grants access to
//     that one server only. Global admins manage these per-server passwords
//     from the GUI; they can also be seeded from the environment.
//
// All persistent state (reboot jobs and the per-server password hashes) lives
// in a single JSON file written atomically, mirroring internal/state.
package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/arumes31/palworld-starter/internal/game"
	"github.com/arumes31/palworld-starter/internal/state"
)

// ScopeAll is the session scope of a global admin: access to every server.
const ScopeAll = "*"

// DefaultLeadSeconds is the default reboot announcement lead time (10 minutes).
const DefaultLeadSeconds = 600

// JobType is the recurrence kind of a reboot job.
type JobType string

const (
	// JobOnce fires a single time at a fixed local date and time.
	JobOnce JobType = "once"
	// JobDaily fires every day at a fixed local time.
	JobDaily JobType = "daily"
)

// RebootJob is one scheduled reboot for a server.
type RebootJob struct {
	ID          string  `json:"id"`
	ServerID    string  `json:"server_id"`
	Type        JobType `json:"type"`
	Time        string  `json:"time"`         // daily: "15:04"; once: "2006-01-02T15:04" (local)
	LeadSeconds int     `json:"lead_seconds"` // announcement lead before the reboot
	Enabled     bool    `json:"enabled"`
	LastFired   int64   `json:"last_fired"` // unix target this job last fired for (dedupe)
}

// serverAuth is a salted SHA-256 hash of a per-server admin password.
type serverAuth struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}

// configFile is the on-disk shape of the admin state.
type configFile struct {
	Jobs       []*RebootJob           `json:"jobs"`
	ServerAuth map[string]*serverAuth `json:"server_auth"`
}

// ServerRef ties a managed server's id/metadata to its game controller and
// timer state. The web layer builds these from its instances.
type ServerRef struct {
	ID      string
	Name    string
	Address string
	Ctrl    *game.Controller
	State   *state.State
}

// activeReboot tracks an in-progress reboot so it can be shown and cancelled.
type activeReboot struct {
	Cancel    context.CancelFunc
	TargetAt  time.Time
	StartedBy string
}

// Manager owns admin configuration, authentication and the reboot scheduler.
type Manager struct {
	mu       sync.Mutex
	cfg      configFile
	path     string
	servers  map[string]ServerRef
	order    []string
	active   map[string]*activeReboot
	globalPw string // global admin password (raw, from env); empty disables the GUI

	baseCtx context.Context

	// launch starts a reboot for a server; it defaults to startReboot and is
	// overridable in tests to decouple scheduling decisions from Docker.
	launch func(serverID string, countdown int, by string) error
}

// NewManager loads persisted admin state from path and wires it to the given
// servers (in order). globalPassword enables the admin GUI when non-empty.
// seeds maps a server id to a plaintext password used as its initial per-server
// password when none is stored yet.
func NewManager(path string, servers []ServerRef, globalPassword string, seeds map[string]string) *Manager {
	m := &Manager{
		path:     path,
		servers:  make(map[string]ServerRef, len(servers)),
		active:   make(map[string]*activeReboot),
		globalPw: globalPassword,
	}
	for _, s := range servers {
		m.servers[s.ID] = s
		m.order = append(m.order, s.ID)
	}
	m.cfg = load(path)
	if m.cfg.ServerAuth == nil {
		m.cfg.ServerAuth = make(map[string]*serverAuth)
	}

	// Seed per-server passwords from the environment for servers that have
	// none stored yet, so a fresh deployment can ship them via env vars.
	changed := false
	for id, pw := range seeds {
		if pw == "" {
			continue
		}
		if _, ok := m.servers[id]; !ok {
			continue
		}
		if _, ok := m.cfg.ServerAuth[id]; !ok {
			m.cfg.ServerAuth[id] = hashPassword(pw)
			changed = true
		}
	}
	if changed {
		m.save()
	}
	m.launch = m.startReboot
	return m
}

// Enabled reports whether the admin GUI is active (a global password is set).
func (m *Manager) Enabled() bool {
	return m != nil && m.globalPw != ""
}

// Servers returns the managed servers in configured order.
func (m *Manager) Servers() []ServerRef {
	out := make([]ServerRef, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.servers[id])
	}
	return out
}

// Server returns the server ref for id.
func (m *Manager) Server(id string) (ServerRef, bool) {
	s, ok := m.servers[id]
	return s, ok
}

// --- Authentication & scope -------------------------------------------------

// Authenticate resolves a submitted password to an access scope. It returns
// ScopeAll for the global password, or a single server id for a matching
// per-server password. ok is false when nothing matches.
func (m *Manager) Authenticate(password string) (scope string, ok bool) {
	if password == "" || m.globalPw == "" {
		return "", false
	}
	if subtle.ConstantTimeCompare([]byte(password), []byte(m.globalPw)) == 1 {
		return ScopeAll, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Iterate in configured order for deterministic behaviour.
	for _, id := range m.order {
		if a := m.cfg.ServerAuth[id]; a != nil && a.matches(password) {
			return id, true
		}
	}
	return "", false
}

// CanAccess reports whether an authenticated scope may act on serverID.
func CanAccess(scope, serverID string) bool {
	return scope == ScopeAll || scope == serverID
}

// IsGlobal reports whether a scope has global (all-server) access.
func IsGlobal(scope string) bool { return scope == ScopeAll }

// VisibleServers returns the servers a scope may see and act on.
func (m *Manager) VisibleServers(scope string) []ServerRef {
	all := m.Servers()
	if scope == ScopeAll {
		return all
	}
	for _, s := range all {
		if s.ID == scope {
			return []ServerRef{s}
		}
	}
	return nil
}

// SetServerPassword sets (or, with an empty password, clears) the per-server
// admin password. Only global admins should be allowed to call this.
func (m *Manager) SetServerPassword(serverID, password string) error {
	if _, ok := m.servers[serverID]; !ok {
		return fmt.Errorf("unknown server %q", serverID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if password == "" {
		delete(m.cfg.ServerAuth, serverID)
	} else {
		m.cfg.ServerAuth[serverID] = hashPassword(password)
	}
	m.save()
	return nil
}

// HasServerPassword reports whether serverID has a per-server password set.
func (m *Manager) HasServerPassword(serverID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.cfg.ServerAuth[serverID]
	return ok
}

func hashPassword(password string) *serverAuth {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	return &serverAuth{
		Salt: hex.EncodeToString(salt),
		Hash: hashWithSalt(salt, password),
	}
}

func hashWithSalt(salt []byte, password string) string {
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}

func (a *serverAuth) matches(password string) bool {
	salt, err := hex.DecodeString(a.Salt)
	if err != nil {
		return false
	}
	want := hashWithSalt(salt, password)
	return subtle.ConstantTimeCompare([]byte(want), []byte(a.Hash)) == 1
}

// --- Reboot job CRUD --------------------------------------------------------

// Jobs returns the reboot jobs a scope may see, sorted by server then time.
func (m *Manager) Jobs(scope string) []*RebootJob {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*RebootJob, 0, len(m.cfg.Jobs))
	for _, j := range m.cfg.Jobs {
		if CanAccess(scope, j.ServerID) {
			cp := *j
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].ServerID != out[k].ServerID {
			return out[i].ServerID < out[k].ServerID
		}
		return out[i].Time < out[k].Time
	})
	return out
}

// AddJob validates and stores a new reboot job.
func (m *Manager) AddJob(j *RebootJob) (*RebootJob, error) {
	if _, ok := m.servers[j.ServerID]; !ok {
		return nil, fmt.Errorf("unknown server %q", j.ServerID)
	}
	if j.Type != JobOnce && j.Type != JobDaily {
		return nil, fmt.Errorf("invalid schedule type %q", j.Type)
	}
	if _, err := NextTarget(j, time.Now()); err != nil {
		return nil, err
	}
	if j.LeadSeconds <= 0 {
		j.LeadSeconds = DefaultLeadSeconds
	}
	if j.LeadSeconds > 3600 {
		j.LeadSeconds = 3600
	}
	j.ID = newID()
	j.Enabled = true
	j.LastFired = 0

	m.mu.Lock()
	m.cfg.Jobs = append(m.cfg.Jobs, j)
	m.save()
	m.mu.Unlock()
	return j, nil
}

// DeleteJob removes a job if the scope may access it.
func (m *Manager) DeleteJob(scope, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, j := range m.cfg.Jobs {
		if j.ID == id && CanAccess(scope, j.ServerID) {
			m.cfg.Jobs = append(m.cfg.Jobs[:i], m.cfg.Jobs[i+1:]...)
			m.save()
			return true
		}
	}
	return false
}

// ToggleJob flips a job's enabled flag if the scope may access it.
func (m *Manager) ToggleJob(scope, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, j := range m.cfg.Jobs {
		if j.ID == id && CanAccess(scope, j.ServerID) {
			j.Enabled = !j.Enabled
			if j.Enabled {
				j.LastFired = 0
			}
			m.save()
			return true
		}
	}
	return false
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Persistence ------------------------------------------------------------

func load(path string) configFile {
	var cfg configFile
	f, err := os.Open(path) // #nosec G304 -- path is server config, not user input
	if err != nil {
		return cfg
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		log.Printf("admin: could not read %s: %v", path, err)
	}
	return cfg
}

// save persists the config atomically. Callers must hold m.mu.
func (m *Manager) save() {
	if m.path == "" {
		return
	}
	dir := filepath.Dir(m.path)
	_ = os.MkdirAll(dir, 0o755)

	tmp := m.path + ".tmp"
	f, err := os.Create(tmp) // #nosec G304 -- path is server config, not user input
	if err != nil {
		log.Printf("admin: cannot write %s: %v", tmp, err)
		return
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m.cfg); err != nil {
		log.Printf("admin: encode failed: %v", err)
		_ = f.Close()
		return
	}
	if err := f.Close(); err != nil {
		log.Printf("admin: close failed: %v", err)
		return
	}
	if err := os.Rename(tmp, m.path); err != nil {
		log.Printf("admin: rename failed: %v", err)
	}
}
