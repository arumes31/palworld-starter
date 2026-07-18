package admin

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T, global string) *Manager {
	t.Helper()
	refs := []ServerRef{{ID: "alpha", Name: "Alpha"}, {ID: "beta", Name: "Beta"}}
	return NewManager(filepath.Join(t.TempDir(), "admin.json"), refs, global, nil)
}

func TestAnnounceOffsets(t *testing.T) {
	offs := announceOffsets(600)
	if len(offs) == 0 {
		t.Fatal("expected offsets")
	}
	if offs[0] != 600 {
		t.Errorf("first offset = %d, want 600", offs[0])
	}
	if offs[len(offs)-1] != 1 {
		t.Errorf("last offset = %d, want 1", offs[len(offs)-1])
	}
	// Strictly descending.
	for i := 1; i < len(offs); i++ {
		if offs[i] >= offs[i-1] {
			t.Fatalf("offsets not strictly descending at %d: %v", i, offs)
		}
	}
	set := map[int]bool{}
	for _, o := range offs {
		set[o] = true
	}
	// Minute marks (10..1), the 10-second marks under a minute, and every
	// second under 30 must all be present.
	for _, want := range []int{600, 540, 120, 60, 50, 40, 30, 29, 10, 5, 1} {
		if !set[want] {
			t.Errorf("missing announcement mark %d", want)
		}
	}
	if set[45] {
		t.Errorf("unexpected 45s mark (should not announce there)")
	}

	// A short countdown must never announce beyond its own length.
	for _, o := range announceOffsets(45) {
		if o > 45 {
			t.Errorf("offset %d exceeds countdown 45", o)
		}
	}
}

func TestRebootMessage(t *testing.T) {
	cases := map[int]string{
		600: "Server reboot in 10 minutes!",
		60:  "Server reboot in 1 minute!",
		30:  "Server reboot in 30 seconds!",
		1:   "Server reboot in 1 second!",
	}
	for in, want := range cases {
		if got := rebootMessage(in); got != want {
			t.Errorf("rebootMessage(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestNextTargetDaily(t *testing.T) {
	now := time.Date(2026, 7, 17, 3, 0, 0, 0, time.Local)
	j := &RebootJob{Type: JobDaily, Time: "04:00"}
	target, err := NextTarget(j, now)
	if err != nil {
		t.Fatal(err)
	}
	if target.Hour() != 4 || target.Day() != 17 {
		t.Errorf("expected today 04:00, got %v", target)
	}

	now = time.Date(2026, 7, 17, 5, 0, 0, 0, time.Local)
	target, _ = NextTarget(j, now)
	if target.Day() != 18 {
		t.Errorf("expected tomorrow after the slot passed, got %v", target)
	}
}

func TestNextTargetOnce(t *testing.T) {
	j := &RebootJob{Type: JobOnce, Time: "2030-01-02T15:04"}
	target, err := NextTarget(j, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if target.Year() != 2030 || target.Hour() != 15 || target.Minute() != 4 {
		t.Errorf("unexpected once target %v", target)
	}

	bad := &RebootJob{Type: JobOnce, Time: "not-a-date"}
	if _, err := NextTarget(bad, time.Now()); err == nil {
		t.Error("expected error for invalid once time")
	}
}

func TestAuthenticateScopes(t *testing.T) {
	m := newTestManager(t, "globalpw")
	if err := m.SetServerPassword("alpha", "alphapw"); err != nil {
		t.Fatal(err)
	}

	if scope, ok := m.Authenticate("globalpw"); !ok || scope != ScopeAll {
		t.Errorf("global auth = (%q,%v), want (*,true)", scope, ok)
	}
	if scope, ok := m.Authenticate("alphapw"); !ok || scope != "alpha" {
		t.Errorf("alpha auth = (%q,%v), want (alpha,true)", scope, ok)
	}
	if _, ok := m.Authenticate("wrong"); ok {
		t.Error("wrong password authenticated")
	}
	if _, ok := m.Authenticate(""); ok {
		t.Error("empty password authenticated")
	}

	// A per-server password must not grant access to another server.
	if CanAccess("alpha", "beta") {
		t.Error("alpha scope must not access beta")
	}
	if !CanAccess(ScopeAll, "beta") {
		t.Error("global scope must access beta")
	}
	if !CanAccess("alpha", "alpha") {
		t.Error("alpha scope must access alpha")
	}
}

func TestAuthenticateDisabledWhenNoGlobal(t *testing.T) {
	m := newTestManager(t, "")
	if m.Enabled() {
		t.Error("manager should be disabled without a global password")
	}
	_ = m.SetServerPassword("alpha", "alphapw")
	if _, ok := m.Authenticate("alphapw"); ok {
		t.Error("auth must be refused while the GUI is disabled")
	}
}

func TestPasswordPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.json")
	refs := []ServerRef{{ID: "alpha"}}

	m1 := NewManager(path, refs, "g", nil)
	if err := m1.SetServerPassword("alpha", "secret"); err != nil {
		t.Fatal(err)
	}

	// A fresh manager must load the stored hash and authenticate against it.
	m2 := NewManager(path, refs, "g", nil)
	if !m2.HasServerPassword("alpha") {
		t.Fatal("password not persisted")
	}
	if scope, ok := m2.Authenticate("secret"); !ok || scope != "alpha" {
		t.Errorf("reloaded auth = (%q,%v)", scope, ok)
	}

	// Clearing removes it.
	_ = m2.SetServerPassword("alpha", "")
	if m2.HasServerPassword("alpha") {
		t.Error("password not cleared")
	}
}

func TestSeededPasswords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.json")
	refs := []ServerRef{{ID: "alpha"}, {ID: "beta"}}

	m := NewManager(path, refs, "g", map[string]string{"beta": "seeded"})
	if scope, ok := m.Authenticate("seeded"); !ok || scope != "beta" {
		t.Errorf("seeded auth = (%q,%v)", scope, ok)
	}

	// Seeds must not overwrite an existing stored password.
	_ = m.SetServerPassword("beta", "changed")
	m2 := NewManager(path, refs, "g", map[string]string{"beta": "seeded"})
	if _, ok := m2.Authenticate("seeded"); ok {
		t.Error("seed overwrote an existing per-server password")
	}
	if scope, ok := m2.Authenticate("changed"); !ok || scope != "beta" {
		t.Errorf("stored password lost after re-seed: (%q,%v)", scope, ok)
	}
}

func TestJobCRUDAndScope(t *testing.T) {
	m := newTestManager(t, "g")

	job, err := m.AddJob(&RebootJob{ServerID: "alpha", Type: JobDaily, Time: "04:00", LeadSeconds: 600})
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if job.ID == "" || !job.Enabled {
		t.Errorf("new job not initialised: %+v", job)
	}

	// Invalid inputs are rejected.
	if _, err := m.AddJob(&RebootJob{ServerID: "alpha", Type: JobDaily, Time: "nope"}); err == nil {
		t.Error("expected error for bad time")
	}
	if _, err := m.AddJob(&RebootJob{ServerID: "ghost", Type: JobDaily, Time: "04:00"}); err == nil {
		t.Error("expected error for unknown server")
	}

	// A beta-scoped admin can neither see nor delete an alpha job.
	if len(m.Jobs("beta")) != 0 {
		t.Error("beta scope must not see alpha jobs")
	}
	if m.DeleteJob("beta", job.ID) {
		t.Error("beta scope must not delete alpha job")
	}
	if len(m.Jobs(ScopeAll)) != 1 {
		t.Error("global scope must see the alpha job")
	}

	// Toggle then delete under an authorised scope.
	if !m.ToggleJob("alpha", job.ID) {
		t.Error("alpha scope should toggle its own job")
	}
	if !m.DeleteJob(ScopeAll, job.ID) {
		t.Error("global delete failed")
	}
	if len(m.Jobs(ScopeAll)) != 0 {
		t.Error("job not deleted")
	}
}

// schedulingClock returns a fixed "now" and the target unix for a daily 05:00
// job, with now safely inside the default 10-minute window.
func schedulingClock() (now time.Time, target int64) {
	now = time.Date(2026, 7, 18, 4, 52, 0, 0, time.Local)
	target = time.Date(2026, 7, 18, 5, 0, 0, 0, time.Local).Unix()
	return now, target
}

func TestClaimDueDifferentServersSameTime(t *testing.T) {
	m := newTestManager(t, "g")
	now, target := schedulingClock()
	m.cfg.Jobs = []*RebootJob{
		{ID: "a", ServerID: "alpha", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
		{ID: "b", ServerID: "beta", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
	}

	due := m.claimDueJobs(now)
	if len(due) != 2 {
		t.Fatalf("two distinct servers at the same time must both fire, got %d: %+v", len(due), due)
	}
	got := map[string]bool{due[0].serverID: true, due[1].serverID: true}
	if !got["alpha"] || !got["beta"] {
		t.Errorf("expected both alpha and beta, got %+v", due)
	}
	for _, j := range m.cfg.Jobs {
		if j.LastFired != target {
			t.Errorf("job %s occurrence not recorded: LastFired=%d", j.ID, j.LastFired)
		}
	}
	// A later tick in the same window must not fire again.
	if again := m.claimDueJobs(now.Add(2 * time.Minute)); len(again) != 0 {
		t.Errorf("jobs re-fired within the same occurrence: %+v", again)
	}
}

func TestClaimDueSameServerDuplicateFiresOnce(t *testing.T) {
	m := newTestManager(t, "g")
	now, target := schedulingClock()
	m.cfg.Jobs = []*RebootJob{
		{ID: "a", ServerID: "alpha", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
		{ID: "b", ServerID: "alpha", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
	}

	due := m.claimDueJobs(now)
	if len(due) != 1 {
		t.Fatalf("two duplicate schedules on one server must fire exactly once, got %d", len(due))
	}
	// BOTH duplicates must be consumed so the skipped one cannot re-fire and
	// double-reboot the server on a later tick.
	for _, j := range m.cfg.Jobs {
		if j.LastFired != target {
			t.Errorf("duplicate job %s not consumed: LastFired=%d", j.ID, j.LastFired)
		}
	}
	if again := m.claimDueJobs(now.Add(1 * time.Minute)); len(again) != 0 {
		t.Errorf("duplicate re-fired, would double-reboot: %+v", again)
	}
}

func TestClaimDueSkipsBusyServerWithoutConsuming(t *testing.T) {
	m := newTestManager(t, "g")
	now, _ := schedulingClock()
	m.cfg.Jobs = []*RebootJob{
		{ID: "a", ServerID: "alpha", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
	}
	m.active["alpha"] = &activeReboot{} // a reboot is already running

	if due := m.claimDueJobs(now); len(due) != 0 {
		t.Fatalf("a busy server must not be scheduled again, got %+v", due)
	}
	if m.cfg.Jobs[0].LastFired != 0 {
		t.Errorf("a job skipped for a busy server must stay eligible, LastFired=%d", m.cfg.Jobs[0].LastFired)
	}
}

func TestClaimDueOutsideWindow(t *testing.T) {
	m := newTestManager(t, "g")
	// now is well before the 10-minute window (target 05:00, window from 04:50).
	now := time.Date(2026, 7, 18, 4, 30, 0, 0, time.Local)
	m.cfg.Jobs = []*RebootJob{
		{ID: "a", ServerID: "alpha", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
	}
	if due := m.claimDueJobs(now); len(due) != 0 {
		t.Errorf("job outside its lead window must not fire, got %+v", due)
	}
}

func TestTickLaunchesEachDueServer(t *testing.T) {
	m := newTestManager(t, "g")
	var launched []string
	m.launch = func(serverID string, countdown int, by string) error {
		launched = append(launched, serverID)
		return nil
	}
	now, _ := schedulingClock()
	m.cfg.Jobs = []*RebootJob{
		{ID: "a", ServerID: "alpha", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
		{ID: "b", ServerID: "beta", Type: JobDaily, Time: "05:00", LeadSeconds: 600, Enabled: true},
	}

	m.tick(now)
	if len(launched) != 2 {
		t.Fatalf("tick should launch both servers, launched %v", launched)
	}
}

func TestVisibleServers(t *testing.T) {
	m := newTestManager(t, "g")
	if len(m.VisibleServers(ScopeAll)) != 2 {
		t.Error("global should see both servers")
	}
	vis := m.VisibleServers("beta")
	if len(vis) != 1 || vis[0].ID != "beta" {
		t.Errorf("beta scope visibility = %+v", vis)
	}
	if len(m.VisibleServers("ghost")) != 0 {
		t.Error("unknown scope should see nothing")
	}
}
