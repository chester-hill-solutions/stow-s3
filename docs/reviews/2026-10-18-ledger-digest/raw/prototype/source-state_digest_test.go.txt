package capacity

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"runtime"
	"testing"
)

func referenceVersionOneState(t *testing.T, state *hostState) []byte {
	t.Helper()
	original := *state
	original.Integrity = ""
	encoded, err := json.Marshal(&original)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append([]byte("stow-capacity-v1\x00"), encoded...))
	original.Integrity = hex.EncodeToString(sum[:])
	encoded, err = json.Marshal(&original)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestStateDigestWireCompatibility(t *testing.T) {
	for _, count := range []int{0, 16, maxResources} {
		ns, _ := resourceCountNamespace(t, [2]int{count / 2, count - count/2})
		state, err := ns.host.read()
		if err != nil {
			t.Fatal(err)
		}
		state.Namespaces[ns.id].Stores["left"].Root = "/é/κ/😀/<&>"
		want := referenceVersionOneState(t, state)
		got, err := encodeState(state)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("version-one bytes changed for %d resources: %v", count, err)
		}
	}
}

func TestStateDigestAllocationBound(t *testing.T) {
	ns, _ := resourceCountNamespace(t, [2]int{maxResources / 2, maxResources / 2})
	state, err := ns.host.read()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	got := stateIntegrity(encoded)
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	budget := uint64(65536)
	t.Logf("integrity input bytes=%d allocated bytes=%d budget=%d", len(encoded), allocated, budget)
	if allocated > budget {
		t.Errorf("state digest duplicates encoded payload: allocated=%d budget=%d", allocated, budget)
	}
	want := sha256.Sum256(append([]byte("stow-capacity-v1\x00"), encoded...))
	if got != hex.EncodeToString(want[:]) {
		t.Fatal("digest changed version-one prefix or bytes")
	}
}
