package workspace_test

import (
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage/workspace"
)

// A hold given a deadline stops retaining anything at it. ExpiresAt was stored
// and round-tripped but never enforced, so a passed expiry still blocked deletion
// forever.
func TestAnExpiredRecoveryHoldStopsBlocking(t *testing.T) {
	registry := registryIn(t)
	id, _ := registerOwned(t, registry, 0)
	request := workspace.RecoveryHoldRequest{
		ID:         "rh-test",
		Owner:      "owner",
		ExpiresAt:  time.Now().Add(-time.Hour),
		References: []workspace.RecoveryReference{{WorkspaceID: id}},
	}
	if _, err := registry.AdmitRecoveryHold(request, ""); err != nil {
		t.Fatalf("admit expired hold: %v", err)
	}
	if err := registry.CheckWorkspaceRecoveryHolds(id); err != nil {
		t.Fatalf("an expired hold still blocked: %v", err)
	}
	live := request
	live.ID = "rh-live"
	live.ExpiresAt = time.Now().Add(time.Hour)
	if _, err := registry.AdmitRecoveryHold(live, ""); err != nil {
		t.Fatalf("admit live hold: %v", err)
	}
	if err := registry.CheckWorkspaceRecoveryHolds(id); err == nil {
		t.Fatal("a live hold did not block")
	}
}
