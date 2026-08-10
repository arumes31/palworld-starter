//go:build unix

package admin

import "testing"

func TestSyncParentDirectory(t *testing.T) {
	if err := syncParentDirectory(t.TempDir()); err != nil {
		t.Fatalf("syncParentDirectory: %v", err)
	}
}
