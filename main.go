package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/arumes31/palworld-starter/internal/discord"
	"github.com/arumes31/palworld-starter/internal/game"
	"github.com/arumes31/palworld-starter/internal/state"
	"github.com/arumes31/palworld-starter/internal/web"
)

// legacyTimeFilePath is the state file of the original single-server setup;
// it is kept for the default server so upgrades do not lose the timer.
const legacyTimeFilePath = "gamecontroller-palworld-time_remaining.json"

func init() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
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
		stateDir := envOr("STATE_DIR", "/hostmem")
		restHost := envOr("REST_API_HOST", "host.docker.internal")
		return []*web.Instance{{
			ID:          "default",
			DisplayName: envOr("DOCKER_CONTAINER_NAME", "Palworld Server"),
			Address:     envOr("SERVER_ADDRESS", "80.66.59.216:8211"),
			Game:        game.NewController(containerName, restHost, 8212, os.Getenv("ADMIN_PASSWORD")),
			State:       state.New(filepath.Join(stateDir, legacyTimeFilePath)),
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
		if raw := os.Getenv("SERVER_" + key + "_RESTPORT"); raw != "" {
			p, err := strconv.Atoi(raw)
			if err != nil || p < 1 || p > 65535 {
				log.Fatalf("SERVER_%s_RESTPORT is not a valid port: %q", key, raw)
			}
			restPort = p
		}
		containerName := envOr("SERVER_"+key+"_CONTAINER", id)
		stateDir := envOr("STATE_DIR", "/hostmem")
		restHost := envOr("SERVER_"+key+"_RESTHOST", "host.docker.internal")
		adminPassword := envOr("SERVER_"+key+"_ADMIN_PASSWORD", os.Getenv("ADMIN_PASSWORD"))

		instances = append(instances, &web.Instance{
			ID:          id,
			DisplayName: envOr("SERVER_"+key+"_NAME", id),
			Address:     envOr("SERVER_"+key+"_ADDRESS", ""),
			Game:        game.NewController(containerName, restHost, restPort, adminPassword),
			State:       state.New(filepath.Join(stateDir, fmt.Sprintf("gamecontroller-%s-time_remaining.json", id))),
		})
	}
	if len(instances) == 0 {
		log.Fatalf("SERVERS is set but contains no server ids")
	}
	return instances
}

// runTicker runs fn on every tick until the context is cancelled.
func runTicker(ctx context.Context, interval time.Duration, fn func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
}

// startTimerTicker counts the remaining time down, warns players in-game at
// the 10/5/1 minute marks and stops the container on expiry.
func startTimerTicker(ctx context.Context, ctrl *game.Controller, st *state.State) {
	runTicker(ctx, 30*time.Second, func() {
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
			return
		}

		if warnMinutes > 0 {
			// Broadcast is player-gated, so this only reaches populated servers.
			ctrl.Broadcast(fmt.Sprintf("Server stops in %d minute(s)! Add time at %s", warnMinutes, game.WebsiteURL()))
		}
	})
}

// startPlayerExtendTicker grants +5 minutes for every 5-minute interval with
// players online, capped at 48 hours.
func startPlayerExtendTicker(ctx context.Context, ctrl *game.Controller, st *state.State) {
	runTicker(ctx, 300*time.Second, func() {
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
	})
}

// startBroadcastScheduler sends the website URL in-game 10 minutes after the
// server becomes populated, then every hour while players stay online. The
// cycle resets when the server empties.
func startBroadcastScheduler(ctx context.Context, ctrl *game.Controller) {
	var nextBroadcast time.Time
	populated := false
	runTicker(ctx, 60*time.Second, func() {
		if len(ctrl.Players()) == 0 {
			populated = false
			return
		}
		if !populated {
			populated = true
			nextBroadcast = time.Now().Add(10 * time.Minute)
		}
		if time.Now().After(nextBroadcast) {
			ctrl.Broadcast("to start this server visit " + game.WebsiteURL())
			nextBroadcast = time.Now().Add(1 * time.Hour)
		}
	})
}

func startAutoBackupTicker(ctx context.Context, ctrl *game.Controller) {
	runTicker(ctx, 15*time.Minute, func() {
		ctrl.RunBackup()
	})
}

func startDiscordRefreshTicker(ctx context.Context) {
	runTicker(ctx, 30*time.Minute, func() {
		_ = discord.InviteURL()
	})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	instances := loadInstances()
	for _, inst := range instances {
		log.Printf("Managing server %q (container %s, address %s)", inst.ID, inst.DisplayName, inst.Address)
	}

	// Warm up cache
	_ = discord.InviteURL()

	// Start one ticker set per server
	for _, inst := range instances {
		startTimerTicker(ctx, inst.Game, inst.State)
		startPlayerExtendTicker(ctx, inst.Game, inst.State)
		startBroadcastScheduler(ctx, inst.Game)
		startAutoBackupTicker(ctx, inst.Game)
	}
	startDiscordRefreshTicker(ctx)

	srv := web.New(instances, "templates", "./static")

	// No WriteTimeout: /stop legitimately blocks for the graceful in-game
	// shutdown countdown, which can exceed a minute.
	httpSrv := &http.Server{
		Addr:              "0.0.0.0:5000",
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("Shutdown signal received, stopping web server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Printf("Web server shutdown error: %v", err)
		}
	}()

	log.Println("Palworld Free Server Controller started on :5000")
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server run error: %v", err)
	}
	log.Println("Shutdown complete")
}
