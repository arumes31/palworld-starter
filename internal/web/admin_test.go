package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arumes31/palworld-starter/internal/admin"
)

// newAdminServer builds a web server with the admin GUI enabled for two
// instances and the given global password.
func newAdminServer(t *testing.T, global string) (*Server, []*Instance) {
	t.Helper()
	instances := []*Instance{newInstance(t, "alpha"), newInstance(t, "beta")}
	refs := make([]admin.ServerRef, 0, len(instances))
	for _, inst := range instances {
		refs = append(refs, admin.ServerRef{
			ID:      inst.ID,
			Name:    inst.DisplayName,
			Address: inst.Address,
			Ctrl:    inst.Game,
			State:   inst.State,
		})
	}
	mgr := admin.NewManager(filepath.Join(t.TempDir(), "admin.json"), refs, global, nil)
	return New(instances, testTemplateDir, "../../static", mgr), instances
}

// sessionFromCookie decrypts a session cookie back into SessionData.
func sessionFromCookie(c *http.Cookie) (*SessionData, error) {
	b, err := decryptSession(c.Value)
	if err != nil {
		return nil, err
	}
	var sd SessionData
	if err := json.Unmarshal(b, &sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

// adminCookie builds an encrypted session cookie for a logged-in admin.
func adminCookie(t *testing.T, scope, csrf string) *http.Cookie {
	t.Helper()
	sd := &SessionData{
		AdminScope:   scope,
		AdminExpires: time.Now().Add(time.Hour).Unix(),
		CsrfToken:    csrf,
		Language:     "en",
	}
	b, _ := json.Marshal(sd)
	enc, _ := encryptSession(b)
	return &http.Cookie{Name: "session", Value: enc}
}

func TestAdminRoutesDisabledWithoutPassword(t *testing.T) {
	srv := newTestServer(t) // no manager -> admin disabled
	mux := srv.Routes()

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/admin", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("admin route should 404 when disabled, got %d", rr.Code)
	}
}

func TestAdminDashboardRequiresLogin(t *testing.T) {
	srv, _ := newAdminServer(t, "secret")

	rr := httptest.NewRecorder()
	srv.handleAdminDashboard(rr, httptest.NewRequest("GET", "/admin", nil))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect to login, got %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/admin/login" {
		t.Errorf("expected redirect to /admin/login, got %q", loc)
	}
}

func TestAdminLoginFlow(t *testing.T) {
	srv, _ := newAdminServer(t, "secret")

	// GET login mints a CSRF token in the session cookie.
	rrGet := httptest.NewRecorder()
	srv.handleAdminLogin(rrGet, httptest.NewRequest("GET", "/admin/login", nil))
	if rrGet.Code != http.StatusOK {
		t.Fatalf("login GET = %d", rrGet.Code)
	}
	cookie := rrGet.Result().Cookies()[0]
	sd, err := sessionFromCookie(cookie)
	if err != nil {
		t.Fatalf("bad session cookie: %v", err)
	}
	if sd.CsrfToken == "" {
		t.Fatal("login GET did not set a CSRF token")
	}

	// Wrong password redirects back with an error and no admin scope.
	form := url.Values{"csrf_token": {sd.CsrfToken}, "password": {"nope"}}
	rrBad := httptest.NewRecorder()
	reqBad := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqBad.AddCookie(cookie)
	srv.handleAdminLogin(rrBad, reqBad)
	if rrBad.Code != http.StatusSeeOther || rrBad.Header().Get("Location") != "/admin/login?err=invalid" {
		t.Errorf("bad login = %d %q", rrBad.Code, rrBad.Header().Get("Location"))
	}

	// Correct password logs in with global scope.
	form = url.Values{"csrf_token": {sd.CsrfToken}, "password": {"secret"}}
	rrOk := httptest.NewRecorder()
	reqOk := httptest.NewRequest("POST", "/admin/login", strings.NewReader(form.Encode()))
	reqOk.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqOk.AddCookie(cookie)
	srv.handleAdminLogin(rrOk, reqOk)
	if rrOk.Code != http.StatusSeeOther || rrOk.Header().Get("Location") != "/admin" {
		t.Fatalf("good login = %d %q", rrOk.Code, rrOk.Header().Get("Location"))
	}
	out, _ := sessionFromCookie(rrOk.Result().Cookies()[0])
	if out.AdminScope != admin.ScopeAll {
		t.Errorf("expected global scope, got %q", out.AdminScope)
	}
}

func TestAdminActionScopeEnforced(t *testing.T) {
	srv, _ := newAdminServer(t, "secret")
	csrf := "tok123"
	cookie := adminCookie(t, "alpha", csrf) // scoped to alpha only

	// Acting on beta from an alpha session is forbidden.
	form := url.Values{"csrf_token": {csrf}, "srv": {"beta"}, "action": {"save"}}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	srv.handleAdminAction(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("cross-server action = %d, want 403", rr.Code)
	}

	// Acting on alpha is allowed; the test container is not awake, so the
	// action reports an error via redirect rather than succeeding.
	form = url.Values{"csrf_token": {csrf}, "srv": {"alpha"}, "action": {"announce"}, "message": {"hi"}}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/admin/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	srv.handleAdminAction(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Errorf("in-scope action = %d, want 303", rr.Code)
	}
	if !strings.HasPrefix(rr.Header().Get("Location"), "/admin") {
		t.Errorf("unexpected redirect %q", rr.Header().Get("Location"))
	}
}

func TestAdminActionRequiresAuthAndCsrf(t *testing.T) {
	srv, _ := newAdminServer(t, "secret")

	// No session: unauthorized.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/action", strings.NewReader("action=save&srv=alpha"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleAdminAction(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated action = %d, want 401", rr.Code)
	}

	// Authenticated but wrong CSRF token: forbidden.
	cookie := adminCookie(t, admin.ScopeAll, "realtoken")
	form := url.Values{"csrf_token": {"wrong"}, "srv": {"alpha"}, "action": {"save"}}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/admin/action", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	srv.handleAdminAction(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("bad-CSRF action = %d, want 403", rr.Code)
	}
}

func TestAdminScheduleAddAndList(t *testing.T) {
	srv, _ := newAdminServer(t, "secret")
	csrf := "tok"
	cookie := adminCookie(t, admin.ScopeAll, csrf)

	form := url.Values{
		"csrf_token": {csrf}, "srv": {"alpha"}, "op": {"add"},
		"type": {"daily"}, "time": {"04:00"}, "lead": {"600"},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/admin/schedule", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	srv.handleAdminSchedule(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("schedule add = %d", rr.Code)
	}
	if jobs := srv.admin.Jobs(admin.ScopeAll); len(jobs) != 1 || jobs[0].ServerID != "alpha" {
		t.Errorf("job not stored: %+v", jobs)
	}
}

func TestPublicPlayersIncludesRebootFields(t *testing.T) {
	// The public /api/players feed carries reboot status so the status page can
	// show a reboot-in-progress banner. With no active reboot it must report
	// reboot=false and reboot_target=0 (and tolerate a nil admin manager).
	srv := newTestServer(t) // admin manager is nil here
	rr := httptest.NewRecorder()
	srv.handlePlayers(rr, httptest.NewRequest("GET", "/api/players", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if _, ok := body["reboot"]; !ok {
		t.Error("response missing reboot field")
	}
	if _, ok := body["reboot_target"]; !ok {
		t.Error("response missing reboot_target field")
	}
	if body["reboot"] != false {
		t.Errorf("expected reboot=false, got %v", body["reboot"])
	}
}

func TestAdminPlayersScoped(t *testing.T) {
	srv, _ := newAdminServer(t, "secret")
	cookie := adminCookie(t, "alpha", "tok")

	// Cross-server read is forbidden.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/admin/api/players?srv=beta", nil)
	req.AddCookie(cookie)
	srv.handleAdminPlayers(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("cross-server players read = %d, want 403", rr.Code)
	}

	// In-scope read returns JSON with an (empty) players array.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/admin/api/players?srv=alpha", nil)
	req.AddCookie(cookie)
	srv.handleAdminPlayers(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("in-scope players read = %d", rr.Code)
	}
	var body struct {
		Server  string        `json:"server"`
		Players []interface{} `json:"players"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("bad JSON: %v", err)
	}
	if body.Server != "alpha" || body.Players == nil {
		t.Errorf("unexpected body: %+v", body)
	}
}
