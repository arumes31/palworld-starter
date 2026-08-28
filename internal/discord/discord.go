// Package discord creates and caches server invite links via a Discord bot.
package discord

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	inviteMu        sync.Mutex
	inviteCache     string
	inviteCacheTime time.Time
	refreshing      bool
)

// InviteURL returns a cached invite link and never blocks on the Discord API:
// a stale cache triggers a background refresh while the previous link (or the
// fallback) is returned immediately, so a slow Discord API can not stall page
// renders. Falls back to DISCORD_FALLBACK_URL when the bot is not configured
// or no invite has been created yet.
func InviteURL() string {
	botToken := os.Getenv("DISCORD_BOT_TOKEN")
	guildID := os.Getenv("DISCORD_GUILD_ID")
	channelID := os.Getenv("DISCORD_CHANNEL_ID")
	fallbackURL := os.Getenv("DISCORD_FALLBACK_URL")
	if fallbackURL == "" {
		fallbackURL = "https://discord.gg/XXXXXINVITENOTFOUNDXXXXXX"
	}

	if botToken == "" || guildID == "" || channelID == "" {
		return fallbackURL
	}
	if _, err := strconv.ParseUint(channelID, 10, 64); err != nil {
		return fallbackURL
	}

	inviteMu.Lock()
	cached := inviteCache
	if (cached == "" || time.Since(inviteCacheTime) >= time.Hour) && !refreshing {
		refreshing = true
		go refreshInvite(botToken, channelID)
	}
	inviteMu.Unlock()

	if cached != "" {
		return cached
	}
	return fallbackURL
}

// refreshInvite creates a fresh invite via the Discord API and stores it in
// the cache. Runs in its own goroutine; only one refresh is in flight at a
// time.
func refreshInvite(botToken, channelID string) {
	defer func() {
		inviteMu.Lock()
		refreshing = false
		inviteMu.Unlock()
	}()

	endpoint, err := url.JoinPath("https://discord.com/api/v10", "channels", channelID, "invites")
	if err != nil {
		return
	}
	payload := map[string]interface{}{
		"max_age":   86400,
		"max_uses":  0,
		"temporary": false,
		"unique":    true,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// #nosec G704 -- channelID is validated as an unsigned Discord snowflake and the host is constant.
	req, err := http.NewRequest("POST", endpoint, bytes.NewReader(payloadBytes))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	// #nosec G704 -- the request host is the constant discord.com and redirects are disabled.
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("Discord invite API error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		safeBody := strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return ' '
			}
			return r
		}, string(body))
		log.Printf("Discord invite API status %d: %s", resp.StatusCode, safeBody) // #nosec G706 -- controls removed above.
		return
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}
	code, ok := result["code"].(string)
	if !ok || code == "" {
		return
	}

	inviteMu.Lock()
	inviteCache = "https://discord.gg/" + code
	inviteCacheTime = time.Now()
	inviteMu.Unlock()
}
