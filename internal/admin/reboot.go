package admin

import (
	"context"
	"fmt"
	"log"
	"time"
)

// Start launches the reboot scheduler. It runs until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.baseCtx = ctx
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.tick(time.Now())
			}
		}
	}()
}

// tick launches any reboots whose announcement window has been reached.
func (m *Manager) tick(now time.Time) {
	m.mu.Lock()
	jobs := make([]*RebootJob, len(m.cfg.Jobs))
	copy(jobs, m.cfg.Jobs)
	m.mu.Unlock()

	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		target, err := NextTarget(j, now)
		if err != nil {
			continue
		}
		lead := j.LeadSeconds
		if lead <= 0 {
			lead = DefaultLeadSeconds
		}
		secsToTarget := target.Sub(now).Seconds()
		// Fire once we are inside the announcement lead window before the
		// target, and only once per occurrence.
		if secsToTarget <= 0 || secsToTarget > float64(lead) {
			continue
		}

		m.mu.Lock()
		if j.LastFired == target.Unix() {
			m.mu.Unlock()
			continue
		}
		if _, busy := m.active[j.ServerID]; busy {
			m.mu.Unlock()
			continue
		}
		j.LastFired = target.Unix()
		if j.Type == JobOnce {
			j.Enabled = false
		}
		m.save()
		m.mu.Unlock()

		log.Printf("admin: scheduled reboot %s for server %q in %ds", j.ID, j.ServerID, int(secsToTarget))
		if err := m.startReboot(j.ServerID, int(secsToTarget), "schedule "+j.ID); err != nil {
			log.Printf("admin: could not start scheduled reboot: %v", err)
		}
	}
}

// NextTarget returns the next target (reboot) time for a job relative to now.
func NextTarget(j *RebootJob, now time.Time) (time.Time, error) {
	switch j.Type {
	case JobOnce:
		t, err := time.ParseInLocation("2006-01-02T15:04", j.Time, time.Local)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid date/time %q (expected YYYY-MM-DDTHH:MM)", j.Time)
		}
		return t, nil
	case JobDaily:
		hm, err := time.ParseInLocation("15:04", j.Time, time.Local)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid time %q (expected HH:MM)", j.Time)
		}
		target := time.Date(now.Year(), now.Month(), now.Day(), hm.Hour(), hm.Minute(), 0, 0, time.Local)
		if !now.Before(target) {
			// Today's slot already passed; the next occurrence is tomorrow.
			target = target.Add(24 * time.Hour)
		}
		return target, nil
	default:
		return time.Time{}, fmt.Errorf("invalid schedule type %q", j.Type)
	}
}

// RebootNow starts a reboot with a fresh countdown of lead seconds.
func (m *Manager) RebootNow(serverID string, lead int, by string) error {
	if lead <= 0 {
		lead = DefaultLeadSeconds
	}
	if lead > 3600 {
		lead = 3600
	}
	return m.startReboot(serverID, lead, by)
}

// startReboot spins up the reboot goroutine for a server. countdown is the
// number of seconds from now until the reboot happens (announcements are
// scheduled within it).
func (m *Manager) startReboot(serverID string, countdown int, by string) error {
	ref, ok := m.servers[serverID]
	if !ok {
		return fmt.Errorf("unknown server %q", serverID)
	}
	if ref.Ctrl.CachedStatus() != "running" {
		return fmt.Errorf("server is not running")
	}

	m.mu.Lock()
	if _, busy := m.active[serverID]; busy {
		m.mu.Unlock()
		return fmt.Errorf("a reboot is already in progress for this server")
	}
	base := m.baseCtx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	ar := &activeReboot{
		Cancel:    cancel,
		TargetAt:  time.Now().Add(time.Duration(countdown) * time.Second),
		StartedBy: by,
	}
	m.active[serverID] = ar
	m.mu.Unlock()

	go m.runReboot(ctx, ref, countdown, ar)
	return nil
}

// CancelReboot aborts an in-progress reboot for a server, if any.
func (m *Manager) CancelReboot(serverID string) bool {
	m.mu.Lock()
	ar, ok := m.active[serverID]
	m.mu.Unlock()
	if !ok {
		return false
	}
	ar.Cancel()
	if ref, ok := m.servers[serverID]; ok {
		_ = ref.Ctrl.AnnounceNow("Scheduled server reboot has been cancelled.")
	}
	return true
}

// ActiveReboot returns the target time of an in-progress reboot for serverID.
func (m *Manager) ActiveReboot(serverID string) (target time.Time, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ar, ok := m.active[serverID]; ok {
		return ar.TargetAt, true
	}
	return time.Time{}, false
}

// runReboot performs the announcement countdown and then restarts the server.
func (m *Manager) runReboot(ctx context.Context, ref ServerRef, countdown int, ar *activeReboot) {
	defer func() {
		m.mu.Lock()
		delete(m.active, ref.ID)
		m.mu.Unlock()
	}()

	target := ar.TargetAt

	// Only run the in-game countdown when players are actually online; on an
	// empty (or auto-paused) server there is nobody to warn, so reboot at once.
	if countdown > 0 && len(ref.Ctrl.Players()) > 0 {
		for _, off := range announceOffsets(countdown) {
			fireAt := target.Add(-time.Duration(off) * time.Second)
			if d := time.Until(fireAt); d > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(d):
				}
			}
			if err := ref.Ctrl.AnnounceNow(rebootMessage(off)); err != nil {
				log.Printf("admin: reboot announce failed for %q: %v", ref.ID, err)
			}
		}
		if d := time.Until(target); d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
	}

	select {
	case <-ctx.Done():
		return
	default:
	}

	log.Printf("admin: rebooting server %q (%s)", ref.ID, ar.StartedBy)
	_ = ref.Ctrl.AnnounceNow("Server is rebooting now. Back in a moment!")

	// Stop() saves the world, backs it up when players are online and shuts the
	// game down gracefully before stopping the container.
	if err := ref.Ctrl.Stop(); err != nil {
		log.Printf("admin: reboot stop failed for %q: %v", ref.ID, err)
		return
	}

	// Keep the freshly rebooted server alive for at least an hour so the idle
	// timer does not stop it again within minutes.
	ref.State.UpdateTimeRemaining(func(cur int) int {
		if cur < 3600 {
			return 3600
		}
		return cur
	})

	if err := ref.Ctrl.Start(); err != nil {
		log.Printf("admin: reboot start failed for %q: %v", ref.ID, err)
	}
}

// announceOffsets returns the "seconds remaining" marks at which to broadcast a
// reboot warning, largest (earliest) first, for a countdown of the given length:
//
//   - every whole minute while more than a minute remains (10, 9, ... 1 min),
//   - every 10 seconds under a minute (50s, 40s, 30s),
//   - every second under 30 seconds (30, 29, ... 1s).
func announceOffsets(countdown int) []int {
	var offs []int
	for k := countdown / 60; k >= 1; k-- {
		offs = append(offs, k*60) // 600, 540, ... 60
	}
	for t := 50; t >= 40; t -= 10 { // 50, 40
		if t <= countdown {
			offs = append(offs, t)
		}
	}
	for t := 30; t >= 1; t-- { // 30, 29, ... 1
		if t <= countdown {
			offs = append(offs, t)
		}
	}
	return offs
}

// rebootMessage renders the in-game warning for a given "seconds remaining".
func rebootMessage(secondsLeft int) string {
	if secondsLeft >= 60 {
		mins := secondsLeft / 60
		return fmt.Sprintf("Server reboot in %d minute%s!", mins, plural(mins))
	}
	return fmt.Sprintf("Server reboot in %d second%s!", secondsLeft, plural(secondsLeft))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
