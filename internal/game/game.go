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
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const playersEndpoint = "http://localhost:8212/v1/api/players"

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

	statusMu    sync.Mutex
	statusCache string
	statusTime  time.Time

	playersMu    sync.Mutex
	playersCache []PlayerInfo
	apiUpCache   bool
	playersTime  time.Time
}

// NewController creates a controller for the named container. A Docker init
// failure is logged, not fatal - all methods degrade gracefully.
func NewController(containerName string) *Controller {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("Failed to initialize Docker client: %v", err)
		cli = nil
	}
	return &Controller{cli: cli, containerName: containerName}
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
	hc := &http.Client{Timeout: 4 * time.Second}
	resp, err := hc.Get(playersEndpoint)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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

	exitCode, output, err := c.exec([]string{"rcon-cli", "Broadcast " + message})
	if err != nil {
		log.Printf("RCON broadcast error: %v", err)
		return
	}
	if exitCode != 0 {
		log.Printf("RCON broadcast failed: %s", output)
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

// Stop stops the container, taking a final backup first when the server is
// awake with players. Waking an auto-paused server just to back it up is
// pointless - the auto-pause flow already saved before pausing.
func (c *Controller) Stop() error {
	if c.cli == nil {
		return fmt.Errorf("docker client not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	status := c.Status()
	if status == "running" {
		if !c.IsPaused() && c.HasActivePlayers() {
			_, _, _ = c.exec([]string{"backup"})
		}

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
	return nil
}
