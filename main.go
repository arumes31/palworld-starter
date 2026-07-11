package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"arumes31/palworld-starter/internal/discord"
	"arumes31/palworld-starter/internal/game"
	"arumes31/palworld-starter/internal/state"
	"arumes31/palworld-starter/internal/web"
)

const timeFilePath = "/hostmem/gamecontroller-palworld-time_remaining.json"

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
}

func websiteURL() string {
	if u := os.Getenv("WEBSITE_URL"); u != "" {
		return u
	}
	return "https://pal.wowcraft.pw/"
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
	containerName := os.Getenv("DOCKER_CONTAINER_NAME")
	if containerName == "" {
		containerName = "my_container"
	}

	log.Printf("Initializing Palworld Starter with container: %s", containerName)

	ctrl := game.NewController(containerName)
	st := state.New(timeFilePath)

	// Warm up cache
	_ = discord.InviteURL()

	// Start tickers
	startTimerTicker(ctrl, st)
	startPlayerExtendTicker(ctrl, st)
	startBroadcastScheduler(ctrl)
	startAutoBackupTicker(ctrl)
	startDiscordRefreshTicker()

	srv := web.New(ctrl, st, "templates", "./static")

	log.Println("Palworld Free Server Controller started on :5000")
	if err := http.ListenAndServe("0.0.0.0:5000", srv.Routes()); err != nil {
		log.Fatalf("Server run error: %v", err)
	}
}
