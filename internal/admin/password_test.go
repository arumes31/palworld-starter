package admin

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestMain lowers the PBKDF2 work factor so the suite stays fast.
func TestMain(m *testing.M) {
	pbkdf2Iterations = 1000
	os.Exit(m.Run())
}

func TestPasswordHashUsesPBKDF2(t *testing.T) {
	a := hashPassword("correct horse battery")
	if a.Algo != hashAlgoPBKDF2 || a.Iter != pbkdf2Iterations {
		t.Fatalf("hash = %+v, want pbkdf2 with %d iterations", a, pbkdf2Iterations)
	}
	if !a.matches("correct horse battery") {
		t.Error("hash does not verify its own password")
	}
	if a.matches("correct horse battery!") {
		t.Error("hash verifies a different password")
	}
	if (&serverAuth{Salt: a.Salt, Hash: a.Hash, Algo: "md5"}).matches("correct horse battery") {
		t.Error("unknown algorithm must never verify")
	}
}

func TestSetServerPasswordEnforcesMinimumLength(t *testing.T) {
	m := newTestManager(t, "global-password-long")
	if err := m.SetServerPassword("alpha", "short"); err == nil {
		t.Fatal("short per-server password accepted")
	}
	if m.HasServerPassword("alpha") {
		t.Error("rejected password was stored")
	}
	if err := m.SetServerPassword("alpha", "exactly-12ch"); err != nil {
		t.Fatalf("12-character password rejected: %v", err)
	}
	// Clearing stays possible.
	if err := m.SetServerPassword("alpha", ""); err != nil {
		t.Fatalf("clearing password failed: %v", err)
	}
}

func TestLegacyHashUpgradedOnLogin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "admin.json")
	refs := []ServerRef{{ID: "alpha", Name: "Alpha"}}
	m := NewManager(path, refs, "global-password-long", nil)

	// Simulate an admin.json written by an older version.
	salt := []byte("0123456789abcdef")
	m.mu.Lock()
	m.cfg.ServerAuth["alpha"] = &serverAuth{
		Salt: hex.EncodeToString(salt),
		Hash: legacyHash(salt, "old"),
	}
	m.mu.Unlock()

	scope, revision, ok := m.Authenticate("old")
	if !ok || scope != "alpha" {
		t.Fatalf("legacy login = (%q,%v), want (alpha,true)", scope, ok)
	}
	if !m.ValidateSession(scope, revision) {
		t.Error("session issued during the upgrade is not valid")
	}

	m2 := NewManager(path, refs, "global-password-long", nil)
	a := m2.cfg.ServerAuth["alpha"]
	if a == nil || a.Algo != hashAlgoPBKDF2 {
		t.Fatalf("persisted hash not upgraded: %+v", a)
	}
	if _, _, ok := m2.Authenticate("old"); !ok {
		t.Error("upgraded hash does not verify the original password")
	}
}
