package game

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestPlayersDeriveFromSharedCache verifies that the public Players() and the
// admin AdminPlayers() views are derived from the SAME cached player list, so
// the public status page and the admin dashboard can never disagree about who
// is online. It also guards the privacy property: the public view must not
// carry user/player ids.
func TestPlayersDeriveFromSharedCache(t *testing.T) {
	c := &Controller{}

	// Seed the shared cache and mark it fresh so refreshPlayersLocked returns
	// without trying (and failing) to re-fetch from a non-existent server.
	c.playersMu.Lock()
	c.playersCache = []AdminPlayerInfo{
		{Name: "Alice", Level: 12, UserID: "steam_0001", PlayerID: "p1", Ping: 20},
		{Name: "Bob", Level: 7, UserID: "steam_0002", PlayerID: "p2", Ping: 33},
	}
	c.apiUpCache = true
	c.playersTime = time.Now()
	c.playersMu.Unlock()

	pub := c.Players()
	adm, up := c.AdminPlayers()

	if !up {
		t.Fatal("AdminPlayers reported API down despite a warm cache")
	}
	if len(pub) != len(adm) || len(pub) != 2 {
		t.Fatalf("public (%d) and admin (%d) player counts disagree", len(pub), len(adm))
	}
	if pub[0].Name != "Alice" || pub[0].Level != 12 {
		t.Errorf("public view wrong: %+v", pub[0])
	}
	if adm[0].UserID != "steam_0001" || adm[0].PlayerID != "p1" {
		t.Errorf("admin view missing ids: %+v", adm[0])
	}

	// The public JSON must never leak user/player ids.
	blob, _ := json.Marshal(pub)
	for _, leak := range []string{"userId", "playerId", "steam_0001", "steam_0002"} {
		if strings.Contains(string(blob), leak) {
			t.Errorf("public player JSON leaked %q: %s", leak, blob)
		}
	}
}
