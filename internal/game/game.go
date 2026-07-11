// Package game controls the Palworld server: its Docker container and its
// REST API. All access paths are designed to never wake an auto-paused
// server - the REST API is only queried when the container is provably
// running and unpaused, and exec commands are additionally gated on players
// being online.
package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// PlayerInfo holds the subset of player data that is safe to expose publicly.
// The REST API also returns ip, userId and playerId - those must never leave
// this process.
type PlayerInfo struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
}

// Controller manages one Palworld server container.
type Controller struct {
	cli           *client.Client
	containerName string
	apiBase       string

	statusMu    sync.Mutex
	statusCache string
	statusTime  time.Time

	passwordMu      sync.Mutex
	adminPassword   string
	passwordFromEnv bool

	playersMu    sync.Mutex
	playersCache []PlayerInfo
	apiUpCache   bool
	playersTime  time.Time

	metricsMu    sync.Mutex
	metricsCache ServerMetrics
	metricsTime  time.Time

	infoMu    sync.Mutex
	infoCache ServerInfo
	infoTime  time.Time

	settingsMu    sync.Mutex
	settingsCache ServerSettings
	settingsKnown bool
	settingsTime  time.Time
}

// NewController creates a controller for the named container whose Palworld
// REST API listens on the given host and port. adminPassword authenticates
// REST calls; when empty it is scraped from the container's ADMIN_PASSWORD
// env on the first inspect. A Docker init failure is logged, not fatal - all
// methods degrade gracefully.
func NewController(containerName, restHost string, restPort int, adminPassword string) *Controller {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Failed to initialize Docker client: %v", err)
		cli = nil
	}
	if restHost == "" {
		restHost = "localhost"
	}
	return &Controller{
		cli:             cli,
		containerName:   containerName,
		apiBase:         fmt.Sprintf("http://%s:%d/v1/api", restHost, restPort),
		adminPassword:   adminPassword,
		passwordFromEnv: adminPassword != "",
	}
}

// WebsiteURL is the public control-website address shown in in-game messages.
func WebsiteURL() string {
	if u := os.Getenv("WEBSITE_URL"); u != "" {
		return u
	}
	return "https://freepalworld.wowcraft.pw/"
}

// endpoint returns the full URL of a REST API endpoint, e.g. endpoint("players").
func (c *Controller) endpoint(name string) string {
	return c.apiBase + "/" + name
}

// authorize adds REST API basic auth to the request when a password is known.
func (c *Controller) authorize(req *http.Request) {
	c.passwordMu.Lock()
	pw := c.adminPassword
	c.passwordMu.Unlock()
	if pw != "" {
		req.SetBasicAuth("admin", pw)
	}
}

// Status inspects the container without caching.
func (c *Controller) Status() string {
	if c.cli == nil {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	inspect, err := c.cli.ContainerInspect(ctx, c.containerName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return "exited"
		}
		log.Printf("Docker inspect error: %v", err)
		return "unknown"
	}

	// Fallback for setups that don't configure ADMIN_PASSWORD on this
	// process: scrape it from the game container's environment.
	c.passwordMu.Lock()
	if !c.passwordFromEnv {
		for _, env := range inspect.Config.Env {
			if strings.HasPrefix(env, "ADMIN_PASSWORD=") {
				c.adminPassword = strings.SplitN(env, "=", 2)[1]
			}
		}
	}
	c.passwordMu.Unlock()

	return inspect.State.Status
}

// CachedStatus returns the container status, cached for 30 seconds.
func (c *Controller) CachedStatus() string {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()

	if time.Since(c.statusTime) < 30*time.Second && c.statusCache != "" {
		return c.statusCache
	}

	status := c.Status()
	c.statusCache = status
	c.statusTime = time.Now()
	return status
}

func (c *Controller) invalidateStatusCache() {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	c.statusCache = ""
	c.statusTime = time.Time{}
}

// IsPaused inspects recent container logs for the auto-pause marker. It scans
// a generous window (chatty REST logging must not push the marker out of
// sight) and treats fresh-boot markers as "not paused", so a stale pause line
// from before a container restart is never misread.
func (c *Controller) IsPaused() bool {
	if c.cli == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       "200",
	}

	reader, err := c.cli.ContainerLogs(ctx, c.containerName, options)
	if err != nil {
		return false
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return false
	}

	lines := strings.Split(string(content), "\n")
	paused := false

	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.Contains(line, "[AUTO PAUSE] Paused") {
			paused = true
			break
		}
		if strings.Contains(line, "Wakeup!!!") ||
			strings.Contains(line, "Resumed by") ||
			strings.Contains(line, "Player connected") ||
			strings.Contains(line, "Player disconnected") ||
			strings.Contains(line, "[AUTO PAUSE] Service") ||
			strings.Contains(line, "REST API started") ||
			strings.Contains(line, "Running Palworld dedicated server") {
			return false
		}
	}
	return paused
}

func (c *Controller) fetchPlayers() ([]PlayerInfo, bool) {
	req, _ := http.NewRequest("GET", c.endpoint("players"), nil)
	c.authorize(req)

	hc := &http.Client{Timeout: 4 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("[%s] fetchPlayers error: %v", c.containerName, err)
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[%s] fetchPlayers HTTP %d", c.containerName, resp.StatusCode)
		return nil, false
	}

	var r struct {
		Players []PlayerInfo `json:"players"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, false
	}
	return r.Players, true
}


type ServerMetrics struct {
	ServerFPS int `json:"serverfps"`
	Uptime    int `json:"uptime"`
}

// ServerInfo is the static server identity from /v1/api/info.
type ServerInfo struct {
	Version    string `json:"version"`
	ServerName string `json:"servername"`
}

func (c *Controller) fetchInfo() (ServerInfo, bool) {
	req, _ := http.NewRequest("GET", c.endpoint("info"), nil)
	c.authorize(req)

	hc := &http.Client{Timeout: 4 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return ServerInfo{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ServerInfo{}, false
	}

	var info ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return ServerInfo{}, false
	}
	return info, true
}

// Info returns the game version and name, cached for 10 minutes. Like all
// REST access it never wakes an auto-paused server; the last known info is
// kept while the server is down so the version stays visible.
func (c *Controller) Info() ServerInfo {
	c.infoMu.Lock()
	defer c.infoMu.Unlock()

	if time.Since(c.infoTime) < 10*time.Minute && c.infoCache.Version != "" {
		return c.infoCache
	}

	if c.CachedStatus() == "running" && !c.IsPaused() {
		if info, ok := c.fetchInfo(); ok {
			c.infoCache = info
			c.infoTime = time.Now()
		}
	}
	return c.infoCache
}

func (c *Controller) fetchMetrics() (ServerMetrics, bool) {
	req, _ := http.NewRequest("GET", c.endpoint("metrics"), nil)
	c.authorize(req)

	hc := &http.Client{Timeout: 4 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return ServerMetrics{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ServerMetrics{}, false
	}

	var m ServerMetrics
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return ServerMetrics{}, false
	}
	return m, true
}

func (c *Controller) Metrics() ServerMetrics {
	c.metricsMu.Lock()
	defer c.metricsMu.Unlock()

	if time.Since(c.metricsTime) < 10*time.Second {
		return c.metricsCache
	}

	if c.CachedStatus() == "running" && !c.IsPaused() {
		if m, ok := c.fetchMetrics(); ok {
			c.metricsCache = m
		}
	} else {
		c.metricsCache = ServerMetrics{}
	}
	c.metricsTime = time.Now()
	return c.metricsCache
}

// ServerSettings is the subset of /v1/api/settings the site displays.
type ServerSettings struct {
	IsPvP bool `json:"bIsPvP"`
}

func (c *Controller) fetchSettings() (ServerSettings, bool) {
	req, _ := http.NewRequest("GET", c.endpoint("settings"), nil)
	c.authorize(req)

	hc := &http.Client{Timeout: 4 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return ServerSettings{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ServerSettings{}, false
	}

	var s ServerSettings
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return ServerSettings{}, false
	}
	return s, true
}

// GameMode reports "pvp" or "pve", or "" while the mode is still unknown
// (the server has not been awake since this process started). The last known
// mode is kept while the server is down - the setting is static per world.
// Like all REST access it never wakes an auto-paused server.
func (c *Controller) GameMode() string {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()

	if !c.settingsKnown || time.Since(c.settingsTime) >= 10*time.Minute {
		if c.CachedStatus() == "running" && !c.IsPaused() {
			if s, ok := c.fetchSettings(); ok {
				c.settingsCache = s
				c.settingsKnown = true
				c.settingsTime = time.Now()
			}
		}
	}

	if !c.settingsKnown {
		return ""
	}
	if c.settingsCache.IsPvP {
		return "pvp"
	}
	return "pve"
}

// refreshPlayersLocked refreshes the players cache. Callers must hold
// playersMu. It never wakes an auto-paused server: the REST API is only
// queried when the container is running and not paused.
func (c *Controller) refreshPlayersLocked() {
	if time.Since(c.playersTime) < 10*time.Second {
		return
	}

	var players []PlayerInfo
	apiUp := false
	if c.CachedStatus() == "running" && !c.IsPaused() {
		players, apiUp = c.fetchPlayers()
	}
	c.playersCache = players
	c.apiUpCache = apiUp
	c.playersTime = time.Now()
}

// Players returns the current player list from the cache, refreshing it when
// stale. Website visitors polling this never fan out into extra calls
// against the game server.
func (c *Controller) Players() []PlayerInfo {
	c.playersMu.Lock()
	defer c.playersMu.Unlock()
	c.refreshPlayersLocked()
	return c.playersCache
}

// RestAPIUp reports whether the last players refresh reached the game's REST
// API - i.e. the server is booted and joinable. False while the container is
// stopped, still booting, or auto-paused.
func (c *Controller) RestAPIUp() bool {
	c.playersMu.Lock()
	defer c.playersMu.Unlock()
	c.refreshPlayersLocked()
	return c.apiUpCache
}

func (c *Controller) HasActivePlayers() bool {
	return len(c.Players()) > 0
}

func (c *Controller) exec(cmd []string) (int, string, error) {
	if c.cli == nil {
		return -1, "", fmt.Errorf("docker client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	response, err := c.cli.ContainerExecCreate(ctx, c.containerName, config)
	if err != nil {
		return -1, "", err
	}

	resp, err := c.cli.ContainerExecAttach(ctx, response.ID, container.ExecStartOptions{})
	if err != nil {
		return -1, "", err
	}
	defer resp.Close()

	var out bytes.Buffer
	_, _ = io.Copy(&out, resp.Reader)

	inspect, err := c.cli.ContainerExecInspect(ctx, response.ID)
	if err != nil {
		return -1, out.String(), err
	}

	return inspect.ExitCode, out.String(), nil
}

// Broadcast sends an in-game RCON broadcast, but only to a running, unpaused
// server with players online - broadcasting to an empty server is pointless
// and the RCON exec would wake an auto-paused one.
func (c *Controller) Broadcast(message string) {
	if c.CachedStatus() != "running" || c.IsPaused() {
		log.Println("Broadcast skipped – server paused or not running")
		return
	}
	if !c.HasActivePlayers() {
		log.Println("Broadcast skipped – no players online")
		return
	}

	payload := map[string]string{"message": message}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", c.endpoint("announce"), bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	hc := &http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("REST API broadcast error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("REST API broadcast failed with HTTP %d", resp.StatusCode)
	}
}

// RunBackup triggers a container-level backup when the server is running,
// unpaused and has players online.
func (c *Controller) RunBackup() {
	if c.CachedStatus() != "running" {
		log.Println("Backup skipped: container not running")
		return
	}
	if c.IsPaused() {
		log.Println("Backup skipped: server is auto-paused (no players)")
		return
	}
	if !c.HasActivePlayers() {
		log.Println("Backup skipped: no players online")
		return
	}

	log.Println("Running scheduled backup...")
	exitCode, output, err := c.exec([]string{"backup"})
	if err != nil {
		log.Printf("Backup error: %v", err)
		return
	}
	if exitCode == 0 {
		log.Println("Backup completed successfully")
	} else {
		log.Printf("Backup failed: %s", strings.TrimSpace(output))
	}
}

// Start starts the container.
func (c *Controller) Start() error {
	if c.cli == nil {
		return fmt.Errorf("docker client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := c.cli.ContainerStart(ctx, c.containerName, container.StartOptions{})
	if err == nil {
		c.invalidateStatusCache()
	}
	return err
}

// SaveWorld asks the game to write the world to disk via the REST API. The
// Palworld API rejects POSTs without a Content-Length header with HTTP 411,
// so the request must carry http.NoBody (Go then sends Content-Length: 0).
func (c *Controller) SaveWorld() error {
	req, err := http.NewRequest("POST", c.endpoint("save"), http.NoBody)
	if err != nil {
		return err
	}
	c.authorize(req)

	hc := &http.Client{Timeout: 60 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("save returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// shutdownViaREST asks the game to exit gracefully after waitSeconds,
// broadcasting message to connected players.
func (c *Controller) shutdownViaREST(waitSeconds int, message string) error {
	payload := map[string]interface{}{"waittime": waitSeconds, "message": message}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", c.endpoint("shutdown"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shutdown returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// waitForExit polls the container status until it leaves "running" or the
// timeout elapses.
func (c *Controller) waitForExit(maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if c.Status() != "running" {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// Stop stops the server. An awake server is saved via the REST API, backed up
// when players are online, and shut down gracefully so players get an in-game
// warning; docker stop is only the fallback. An auto-paused server is stopped
// directly - the auto-pause flow already saved, and waking it just to back it
// up is pointless.
func (c *Controller) Stop() error {
	if c.cli == nil {
		return fmt.Errorf("docker client not initialized")
	}

	status := c.Status()
	if status != "running" {
		return nil
	}

	if !c.IsPaused() {
		if err := c.SaveWorld(); err != nil {
			log.Printf("[%s] World save before stop failed: %v", c.containerName, err)
		}

		waitSeconds := 1
		if c.HasActivePlayers() {
			_, _, _ = c.exec([]string{"backup"})
			waitSeconds = 15
		}

		message := fmt.Sprintf("Server pauses in %d seconds! Your data is safe - restart it anytime at %s", waitSeconds, WebsiteURL())
		if err := c.shutdownViaREST(waitSeconds, message); err != nil {
			log.Printf("[%s] REST shutdown failed, falling back to docker stop: %v", c.containerName, err)
		} else if c.waitForExit(time.Duration(waitSeconds+30) * time.Second) {
			c.invalidateStatusCache()
			return nil
		} else {
			log.Printf("[%s] REST shutdown timed out, falling back to docker stop", c.containerName)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	timeout := 10
	stopOpts := container.StopOptions{
		Timeout: &timeout,
	}
	err := c.cli.ContainerStop(ctx, c.containerName, stopOpts)
	if err == nil {
		c.invalidateStatusCache()
	}
	return err
}
