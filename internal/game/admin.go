package game

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AdminPlayerInfo is the player view exposed to authenticated admins only. It
// adds the userId/playerId needed for kick/ban to the public PlayerInfo, but
// still omits the player's IP address, which never leaves this process.
type AdminPlayerInfo struct {
	Name     string  `json:"name"`
	Level    int     `json:"level"`
	UserID   string  `json:"userId"`
	PlayerID string  `json:"playerId"`
	Ping     float64 `json:"ping"` // Palworld reports ping as a float (ms)
}

// awake reports whether the REST API can be reached without waking an
// auto-paused server: the container is running and not auto-paused.
func (c *Controller) awake() bool {
	return c.CachedStatus() == "running" && !c.IsPaused()
}

// postJSON sends a JSON body to a REST endpoint and returns an error when the
// call cannot be made or the server answers with a non-200 status.
func (c *Controller) postJSON(name string, payload interface{}) error {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequest("POST", c.endpoint(name), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	hc := &http.Client{Timeout: 8 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", name, resp.StatusCode)
	}
	return nil
}

// AnnounceNow sends an in-game broadcast on explicit admin request. Unlike the
// player-gated Broadcast it returns an error so the GUI can report failures,
// but it still refuses to run against a stopped or auto-paused server (there is
// nobody to reach and the call would needlessly wake it).
func (c *Controller) AnnounceNow(message string) error {
	if !c.awake() {
		return fmt.Errorf("server is not awake")
	}
	return c.postJSON("announce", map[string]string{"message": message})
}

// Kick removes a player from the running server. userID is the REST API's
// player user id (e.g. "steam_0123...").
func (c *Controller) Kick(userID, message string) error {
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	if !c.awake() {
		return fmt.Errorf("server is not awake")
	}
	if message == "" {
		message = "You have been kicked from the server."
	}
	return c.postJSON("kick", map[string]string{"userid": userID, "message": message})
}

// Ban bans a player from the server and disconnects them.
func (c *Controller) Ban(userID, message string) error {
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	if !c.awake() {
		return fmt.Errorf("server is not awake")
	}
	if message == "" {
		message = "You have been banned from the server."
	}
	return c.postJSON("ban", map[string]string{"userid": userID, "message": message})
}

// Unban lifts a ban for the given user id.
func (c *Controller) Unban(userID string) error {
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	if !c.awake() {
		return fmt.Errorf("server is not awake")
	}
	return c.postJSON("unban", map[string]string{"userid": userID})
}

// AdminPlayers returns the full player list including user ids for kick/ban,
// plus whether the game's REST API is reachable. It reads the SAME shared
// cache the public Players()/RestAPIUp() use, so the admin dashboard and the
// public status page can never disagree about who is online. Like all REST
// access it never wakes an auto-paused server.
func (c *Controller) AdminPlayers() ([]AdminPlayerInfo, bool) {
	c.playersMu.Lock()
	defer c.playersMu.Unlock()
	c.refreshPlayersLocked()
	out := make([]AdminPlayerInfo, len(c.playersCache))
	copy(out, c.playersCache)
	return out, c.apiUpCache
}
