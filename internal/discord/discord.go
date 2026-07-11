// Package discord creates and caches server invite links via a Discord bot.
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	inviteCache     string
	inviteCacheTime time.Time
	inviteCacheMu   sync.Mutex
)

// InviteURL returns a cached invite link, creating a fresh one via the
// Discord API at most once per hour. Falls back to DISCORD_FALLBACK_URL when
// the bot is not configured or the API call fails.
func InviteURL() string {
	inviteCacheMu.Lock()
	defer inviteCacheMu.Unlock()

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

	if inviteCache != "" && time.Since(inviteCacheTime) < 1*time.Hour {
		return inviteCache
	}

	url := fmt.Sprintf("https://discord.com/api/v10/channels/%s/invites", channelID)
	payload := map[string]interface{}{
		"max_age":   86400,
		"max_uses":  0,
		"temporary": false,
		"unique":    true,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fallbackURL
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return fallbackURL
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	req.Header.Set("Content-Type", "application/json")

	hc := &http.Client{Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		log.Printf("Discord invite API error: %v", err)
		return fallbackURL
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
			if code, ok := result["code"].(string); ok && code != "" {
				inviteURL := "https://discord.gg/" + code
				inviteCache = inviteURL
				inviteCacheTime = time.Now()
				return inviteURL
			}
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Discord invite API status %d: %s", resp.StatusCode, string(body))
	}

	return fallbackURL
}
