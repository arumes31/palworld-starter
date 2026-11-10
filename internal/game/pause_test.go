package game

import "testing"

func TestPausedFromLogs(t *testing.T) {
	cases := []struct {
		name string
		logs string
		want bool
	}{
		{"empty", "", false},
		{"wrapper pause marker", "Running Palworld dedicated server\n[AUTO PAUSE] Paused\n", true},
		{"woken after pause", "[AUTO PAUSE] Paused\nWakeup!!!\n", false},
		{"fresh boot after stale pause", "[AUTO PAUSE] Paused\nRunning Palworld dedicated server\n", false},
		{"marker in display name on join", "[AUTO PAUSE] Service started\n[LOG] [AUTO PAUSE] Paused 1.2.3.4 connected the server. (User id: steam_1)\n", false},
		{"marker in wrapper join line", "Player connected: [AUTO PAUSE] Paused\n", false},
		{"marker in chat ignored", "REST API started\n[CHAT] <bob> [AUTO PAUSE] Paused\n", false},
		{"chat after real pause keeps pause", "[AUTO PAUSE] Paused\n[CHAT] <bob> hi\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pausedFromLogs(tc.logs); got != tc.want {
				t.Errorf("pausedFromLogs(%q) = %v, want %v", tc.logs, got, tc.want)
			}
		})
	}
}
