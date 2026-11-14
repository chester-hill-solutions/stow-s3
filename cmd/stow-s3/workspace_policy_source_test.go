package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/internal/policystore"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestCapturePrincipalReadsARevokedSuccessor(t *testing.T) {
	registry, id, _ := cliWorkspace(t)
	path := persistedPolicy(t, registry, id, policy.Allow)
	principal, err := capturePrincipal(path)
	if err != nil {
		t.Fatalf("capture principal: %v", err)
	}
	if _, err := stow.CheckpointOfAuthorized(t.Context(), registry, id, stow.CheckpointOptions{}, principal); err != nil {
		t.Fatalf("capture under initial allow: %v", err)
	}
	beforeIDs := checkpointIDs(t, registry)

	store, err := policystore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := policy.FromSet(checkpointCapturePolicy(id, policy.Deny, "rev-cli-capture-next", time.Time{}), current.Sequence, time.Now(), authority.All())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Persist(revoked, current.Sequence); err != nil {
		t.Fatal(err)
	}
	_, err = stow.CheckpointOfAuthorized(t.Context(), registry, id, stow.CheckpointOptions{}, principal)
	if !policy.IsRefusal(err) {
		t.Fatalf("capture after successor revision = %v, want policy refusal", err)
	}
	if got := checkpointIDs(t, registry); !sameStrings(got, beforeIDs) {
		t.Fatalf("revoked capture changed checkpoints: got %v, want %v", got, beforeIDs)
	}
}

func TestCapturePrincipalReportsDamagedPolicyAsUnknown(t *testing.T) {
	registry, id, _ := cliWorkspace(t)
	path := persistedPolicy(t, registry, id, policy.Allow)
	principal, err := capturePrincipal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = stow.CheckpointOfAuthorized(t.Context(), registry, id, stow.CheckpointOptions{}, principal)
	if !errors.Is(err, policy.ErrRecordDamaged) || policy.IsRefusal(err) {
		t.Fatalf("capture with damaged policy = %v, want unknown ErrRecordDamaged", err)
	}
	if got := checkpointIDs(t, registry); len(got) != 0 {
		t.Fatalf("damaged policy capture created checkpoints: %v", got)
	}
}

func TestCapturePrincipalReportsRemovedPolicyAsNoRecord(t *testing.T) {
	registry, id, _ := cliWorkspace(t)
	path := persistedPolicy(t, registry, id, policy.Allow)
	principal, err := capturePrincipal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	_, err = stow.CheckpointOfAuthorized(t.Context(), registry, id, stow.CheckpointOptions{}, principal)
	if !errors.Is(err, policy.ErrNoRecord) || policy.IsRefusal(err) {
		t.Fatalf("capture with removed policy = %v, want unknown ErrNoRecord", err)
	}
	if got := checkpointIDs(t, registry); len(got) != 0 {
		t.Fatalf("missing policy capture created checkpoints: %v", got)
	}
}

func TestCapturePrincipalRefusesExpiredPersistedPolicy(t *testing.T) {
	registry, id, _ := cliWorkspace(t)
	path := filepath.Join(t.TempDir(), "expired-policy.json")
	store, err := policystore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	issued := time.Now().Add(-2 * time.Hour)
	store.SetClock(func() time.Time { return issued })
	expires := issued.Add(time.Hour)
	record, err := policy.FromSet(checkpointCapturePolicy(id, policy.Allow, "rev-expired-cli", expires), 0, issued, authority.All())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Persist(record, 0); err != nil {
		t.Fatal(err)
	}
	principal, err := capturePrincipal(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = stow.CheckpointOfAuthorized(t.Context(), registry, id, stow.CheckpointOptions{}, principal)
	if !errors.Is(err, policy.ErrStalePolicy) || policy.IsRefusal(err) {
		t.Fatalf("capture under expired policy = %v, want unknown ErrStalePolicy", err)
	}
	if got := checkpointIDs(t, registry); len(got) != 0 {
		t.Fatalf("expired policy capture created checkpoints: %v", got)
	}
}

func checkpointCapturePolicy(id string, effect policy.Effect, revision string, expires time.Time) policy.Set {
	var set policy.Set
	set.Revision = revision
	set.Expires = expires
	set.Add(policy.LocalNamespace, id,
		policy.Selector{Kind: policy.KindWorkspace}, effect, authority.WorkspaceCapture)
	return set
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
