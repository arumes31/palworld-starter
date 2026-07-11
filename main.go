package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"arumes31/palworld-starter/internal/discord"
	"arumes31/palworld-starter/internal/game"
	"arumes31/palworld-starter/internal/state"
	"arumes31/palworld-starter/internal/web"
)

// legacyTimeFilePath is the state file of the original single-server setup;
// it is kept for the default server so upgrades do not lose the timer.
const legacyTimeFilePath = "/hostmem/gamecontroller-palworld-time_remaining.json"

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func websiteURL() string {
	if u := os.Getenv("WEBSITE_URL"); u != "" {
		return u
	}
	return "https://pal.wowcraft.pw/"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envKey converts a server id to the uppercase form used in env var names
// (e.g. "pal-2" → "PAL_2").
func envKey(id string) string {
	up := strings.ToUpper(id)
	return strings.Map(func(r rune) rune {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, up)
}

// loadInstances builds one web.Instance per configured server.
//
// Multi-server mode: SERVERS is a comma-separated list of server ids. Each
// server is configured via SERVER_<ID>_CONTAINER, SERVER_<ID>_ADDRESS,
// SERVER_<ID>_RESTPORT and SERVER_<ID>_NAME (id in uppercase, non-alphanumeric
// characters replaced by "_").
//
// Single-server mode (SERVERS unset): the legacy DOCKER_CONTAINER_NAME /
// SERVER_ADDRESS variables and REST port 8212 are used unchanged.
func loadInstances() []*web.Instance {
	serverList := os.Getenv("SERVERS")
	if serverList == "" {
		containerName := envOr("DOCKER_CONTAINER_NAME", "my_container")
		return []*web.Instance{{
			ID:          "default",
			DisplayName: envOr("DOCKER_CONTAINER_NAME", "Palworld Server"),
			Address:     envOr("SERVER_ADDRESS", "80.66.59.216:8211"),
			Game:        game.NewController(containerName, 8212),
			State:       state.New(legacyTimeFilePath),
		}}
	}

	var instances []*web.Instance
	for _, raw := range strings.Split(serverList, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		key := envKey(id)

		restPort := 8212
		if p, err := strconv.Atoi(os.Getenv("SERVER_" + key + "_RESTPORT")); err == nil && p > 0 {
			restPort = p
		}
		containerName := envOr("SERVER_"+key+"_CONTAINER", id)

		instances = append(instances, &web.Instance{
			ID:          id,
			DisplayName: envOr("SERVER_"+key+"_NAME", id),
			Address:     envOr("SERVER_"+key+"_ADDRESS", ""),
			Game:        game.NewController(containerName, restPort),
			State:       state.New(fmt.Sprintf("/hostmem/gamecontroller-%s-time_remaining.json", id)),
		})
	}
	if len(instances) == 0 {
		log.Fatalf("SERVERS is set but contains no server ids")
	}
	return instances
}

// startTimerTicker counts the remaining time down, warns players in-game at
// the 10/5/1 minute marks and stops the container on expiry.
func startTimerTicker(ctrl *game.Controller, st *state.State) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		for range ticker.C {
			var expired bool
			warnMinutes := 0
			st.UpdateTimeRemaining(func(current int) int {
				if current <= 0 {
					return 0
				}
				val := current - 30
				if val < 0 {
					val = 0
				}
				if val == 0 {
					expired = true
					return val
				}
				for _, threshold := range []int{600, 300, 60} {
					if current > threshold && val <= threshold {
						warnMinutes = threshold / 60
						break
					}
				}
				return val
			})

			if expired {
				if ctrl.CachedStatus() == "running" {
					log.Println("TIME EXPIRED → stopping container")
					if err := ctrl.Stop(); err != nil {
						log.Printf("Timer container shutdown failed: %v", err)
					}
				}
				continue
			}

			if warnMinutes > 0 {
				// Broadcast is player-gated, so this only reaches populated servers.
				ctrl.Broadcast(fmt.Sprintf("Server stops in %d minute(s)! Add time at %s", warnMinutes, websiteURL()))
			}
		}
	}()
}

// startPlayerExtendTicker grants +5 minutes for every 5-minute interval with
// players online, capped at 48 hours.
func startPlayerExtendTicker(ctrl *game.Controller, st *state.State) {
	ticker := time.NewTicker(300 * time.Second)
	go func() {
		for range ticker.C {
			// Players() only queries the REST API when the server is running
			// and not auto-paused, so this never wakes a sleeping server.
			count := len(ctrl.Players())
			if count > 0 {
				val := st.UpdateTimeRemaining(func(current int) int {
					newVal := current + 300
					if newVal > 172800 { // max 48h
						return 172800
					}
					return newVal
				})
				log.Printf("Players online (%d) → +5 min (now %dh)", count, val/3600)
			}
		}
	}()
}

// startBroadcastScheduler sends the website URL in-game 10 minutes after the
// server becomes populated, then every 3 hours while players stay online.
// The cycle resets when the server empties.
func startBroadcastScheduler(ctrl *game.Controller) {
	ticker := time.NewTicker(60 * time.Second)
	go func() {
		var nextBroadcast time.Time
		populated := false
		for range ticker.C {
			if len(ctrl.Players()) == 0 {
				populated = false
				continue
			}
			if !populated {
				populated = true
				nextBroadcast = time.Now().Add(10 * time.Minute)
			}
			if time.Now().After(nextBroadcast) {
				ctrl.Broadcast("to start this server visit " + websiteURL())
				nextBroadcast = time.Now().Add(3 * time.Hour)
			}
		}
	}()
}

func startAutoBackupTicker(ctrl *game.Controller) {
	ticker := time.NewTicker(15 * time.Minute)
	go func() {
		for range ticker.C {
			ctrl.RunBackup()
		}
	}()
}

func startDiscordRefreshTicker() {
	ticker := time.NewTicker(30 * time.Minute)
	go func() {
		for range ticker.C {
			_ = discord.InviteURL()
		}
	}()
}

func main() {
	instances := loadInstances()
	for _, inst := range instances {
		log.Printf("Managing server %q (container %s, address %s)", inst.ID, inst.DisplayName, inst.Address)
	}

	// Warm up cache
	_ = discord.InviteURL()

	// Start one ticker set per server
	for _, inst := range instances {
		startTimerTicker(inst.Game, inst.State)
		startPlayerExtendTicker(inst.Game, inst.State)
		startBroadcastScheduler(inst.Game)
		startAutoBackupTicker(inst.Game)
	}
	startDiscordRefreshTicker()

	srv := web.New(instances, "templates", "./static")

	log.Println("Palworld Free Server Controller started on :5000")
	if err := http.ListenAndServe("0.0.0.0:5000", srv.Routes()); err != nil {
		log.Fatalf("Server run error: %v", err)
	}
}
