package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/policy"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type checkpointPolicyCase struct {
	name string
	path string
}

func TestPolicyFlagRefusesReceiptBackedCheckpointModes(t *testing.T) {
	registry, id, _ := cliWorkspace(t)
	policies := []checkpointPolicyCase{
		{name: "missing", path: filepath.Join(t.TempDir(), "missing-policy.json")},
		{name: "deny", path: persistedPolicy(t, registry, id, policy.Deny)},
		{name: "allow", path: persistedPolicy(t, registry, id, policy.Allow)},
	}
	requestKey := "existing-receipt"
	original, err := runCheckpointVerb(t, checkpointRequestArgs(id, registry, requestKey)...)
	if err != nil {
		t.Fatalf("unscoped keyed capture: %v", err)
	}
	committed := decodeCheckpointCommandResult(t, original)
	if committed.Result.Outcome != "committed" || committed.Result.Replayed {
		t.Fatalf("initial keyed capture = %+v", committed.Result)
	}
	wantFiles := checkpointSnapshot(t, registry)
	wantIDs := checkpointIDs(t, registry)

	for _, policyCase := range policies {
		checkFreshPolicyRequest(t, registry, id, policyCase)
		checkExistingPolicyReplayAndResolve(t, registry, id, requestKey, policyCase)
		assertCheckpointState(t, registry, wantIDs, wantFiles)
	}

	replayOutput, err := runCheckpointVerb(t, checkpointRequestArgs(id, registry, requestKey)...)
	if err != nil {
		t.Fatalf("unscoped replay: %v", err)
	}
	replayed := decodeCheckpointCommandResult(t, replayOutput)
	if !replayed.Result.Replayed || replayed.Result.Checkpoint.ID != committed.Result.Checkpoint.ID {
		t.Fatalf("unscoped replay = %+v, initial=%+v", replayed.Result, committed.Result)
	}
	resolveArgs := append(checkpointRequestArgs(id, registry, requestKey), "--resolve")
	resolveOutput, err := runCheckpointVerb(t, resolveArgs...)
	if err != nil {
		t.Fatalf("unscoped resolve: %v", err)
	}
	resolved := decodeCheckpointCommandResult(t, resolveOutput)
	if resolved.Result.Outcome != "committed" || resolved.Result.Checkpoint.ID != committed.Result.Checkpoint.ID {
		t.Fatalf("unscoped resolve = %+v, initial=%+v", resolved.Result, committed.Result)
	}
	assertCheckpointState(t, registry, wantIDs, wantFiles)
}

func checkpointRequestArgs(id, registry, key string) []string {
	return []string{"--id", id, "--registry-dir", registry, "--request-key", key, "--timeout", "5s"}
}

func checkFreshPolicyRequest(t *testing.T, registry, id string, policyCase checkpointPolicyCase) {
	t.Helper()
	key := "fresh-" + policyCase.name
	beforeIDs := checkpointIDs(t, registry)
	args := append(checkpointRequestArgs(id, registry, key), "--policy", policyCase.path)
	assertPolicyModeRefused(t, args...)
	if !reflect.DeepEqual(checkpointIDs(t, registry), beforeIDs) {
		t.Fatalf("policy + fresh key created checkpoint metadata for %q", key)
	}
	missing, err := stow.ResolveCheckpoint(t.Context(), registry, id, stow.CheckpointRequest{Key: key})
	if err != nil || missing.Outcome != "not_found" {
		t.Fatalf("fresh request receipt after refusal = %+v, %v; want not_found", missing, err)
	}
}

func checkExistingPolicyReplayAndResolve(t *testing.T, registry, id, key string, policyCase checkpointPolicyCase) {
	t.Helper()
	beforeIDs := checkpointIDs(t, registry)
	beforeFiles := checkpointSnapshot(t, registry)
	replayArgs := append(checkpointRequestArgs(id, registry, key), "--policy", policyCase.path)
	assertPolicyModeRefused(t, replayArgs...)
	assertCheckpointState(t, registry, beforeIDs, beforeFiles)
	resolveArgs := append(checkpointRequestArgs(id, registry, key), "--resolve", "--policy", policyCase.path)
	assertPolicyModeRefused(t, resolveArgs...)
	assertCheckpointState(t, registry, beforeIDs, beforeFiles)
}

func assertPolicyModeRefused(t *testing.T, args ...string) {
	t.Helper()
	output, err := runCheckpointVerb(t, args...)
	if err == nil || !strings.Contains(err.Error(), "request-backed") {
		t.Fatalf("policy request mode error = %v, want explicit unsupported request-backed policy refusal", err)
	}
	if len(output) != 0 {
		t.Fatalf("policy request mode exposed checkpoint metadata: %s", output)
	}
}

func decodeCheckpointCommandResult(t *testing.T, data []byte) captureCommandResult {
	t.Helper()
	var result captureCommandResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode checkpoint command result %q: %v", data, err)
	}
	return result
}

func checkpointIDs(t *testing.T, registry string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(registry, "checkpoints"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.Name())
	}
	return ids
}

func checkpointSnapshot(t *testing.T, registry string) map[string][]byte {
	t.Helper()
	snapshot := make(map[string][]byte)
	for _, id := range checkpointIDs(t, registry) {
		for _, name := range []string{"manifest.json", "receipt.json"} {
			path := filepath.Join(registry, "checkpoints", id, name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			snapshot[id+"/"+name] = data
		}
	}
	return snapshot
}

func assertCheckpointState(t *testing.T, registry string, wantIDs []string, wantFiles map[string][]byte) {
	t.Helper()
	if got := checkpointIDs(t, registry); !reflect.DeepEqual(got, wantIDs) {
		t.Fatalf("checkpoint IDs = %v, want %v", got, wantIDs)
	}
	if got := checkpointSnapshot(t, registry); !reflect.DeepEqual(got, wantFiles) {
		t.Fatalf("checkpoint manifests/receipts changed: got keys %v, want %v", mapKeys(got), mapKeys(wantFiles))
	}
}

func mapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
