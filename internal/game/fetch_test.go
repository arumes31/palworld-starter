package game

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestFetchPlayersDecodesRealPayload drives fetchPlayers against a realistic
// Palworld /v1/api/players response. The regression it guards: Palworld reports
// "ping" as a float (and returns several fields the app does not model). If the
// ping field is typed as int, the whole array fails to decode and fetchPlayers
// reports the REST API as down (players empty, apiUp=false) — which surfaced as
// "REST API unavailable" and 0 players in the UI.
func TestFetchPlayersDecodesRealPayload(t *testing.T) {
	payload := `{"players":[
	  {"name":"JDM","accountName":"JDM","playerId":"aaaa","userId":"steam_11111","ip":"10.0.0.5","ping":24.53,"location_x":100.5,"location_y":200.25,"level":44,"building_count":12},
	  {"name":"GMC1925","accountName":"GMC","playerId":"bbbb","userId":"steam_22222","ip":"10.0.0.6","ping":51,"location_x":0,"location_y":0,"level":46,"building_count":3}
	]}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/api/players" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	defer ts.Close()

	c := &Controller{apiBase: ts.URL + "/v1/api"}

	players, up := c.fetchPlayers()
	if !up {
		t.Fatal("fetchPlayers reported the REST API down on a valid payload (float-ping decode regression)")
	}
	if len(players) != 2 {
		t.Fatalf("decoded %d players, want 2", len(players))
	}
	if players[0].Name != "JDM" || players[0].Level != 44 || players[0].UserID != "steam_11111" || players[0].PlayerID != "aaaa" {
		t.Errorf("player 0 decoded wrong: %+v", players[0])
	}
	if players[0].Ping != 24.53 {
		t.Errorf("float ping not decoded: got %v want 24.53", players[0].Ping)
	}
	// An integer ping must still decode into the float field.
	if players[1].Ping != 51 {
		t.Errorf("integer ping not decoded: got %v want 51", players[1].Ping)
	}
}

// TestFetchPlayersFallbackOnBadAdminField ensures that an unexpected type on an
// admin-only field never knocks out the public status/player list: the fetch
// falls back to name+level and still reports the API as up.
func TestFetchPlayersFallbackOnBadAdminField(t *testing.T) {
	// ping arrives as a string here — a shape the admin struct cannot decode.
	payload := `{"players":[
	  {"name":"JDM","level":44,"userId":"steam_1","ping":"nonsense"},
	  {"name":"Bob","level":7}
	]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, payload)
	}))
	defer ts.Close()

	c := &Controller{apiBase: ts.URL + "/v1/api"}
	players, up := c.fetchPlayers()
	if !up {
		t.Fatal("fallback must keep the REST API reported as up")
	}
	if len(players) != 2 {
		t.Fatalf("got %d players, want 2", len(players))
	}
	if players[0].Name != "JDM" || players[0].Level != 44 {
		t.Errorf("fallback lost name/level: %+v", players[0])
	}
}

// TestFetchPlayersHandlesNon200 confirms a non-200 response is reported as down.
func TestFetchPlayersHandlesNon200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	c := &Controller{apiBase: ts.URL + "/v1/api"}
	if players, up := c.fetchPlayers(); up || players != nil {
		t.Errorf("expected (nil,false) on HTTP 401, got (%v,%v)", players, up)
	}
}
