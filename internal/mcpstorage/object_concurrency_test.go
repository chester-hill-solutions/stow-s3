package mcpstorage

import (
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestObjectConcurrentGuardedWriters(t *testing.T) {
	for _, sameKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "different-keys", true: "same-key"}[sameKey], func(t *testing.T) {
			_, session := objectFixture(t, ObjectConfig{})
			first := readObjectForTest(t, session, "first")
			second := readObjectForTest(t, session, "second")
			inputs := []objectSaveInput{
				saveInputForTest("first", "one", first.ObservationToken, "writer-one"),
				saveInputForTest("second", "two", second.ObservationToken, "writer-two"),
			}
			if sameKey {
				inputs[1] = saveInputForTest("first", "two", first.ObservationToken, "writer-two")
			}
			results := concurrentObjectSaves(t, session, inputs)
			count := 0
			for _, result := range results {
				if result.Outcome == "committed" {
					count++
					continue
				}
				if result.Outcome != "not_committed" || result.Error == nil || result.Error.Code != "conflict" {
					t.Fatalf("concurrent save=%+v", result)
				}
			}
			want := 2
			if sameKey {
				want = 1
			}
			if count != want {
				t.Fatalf("committed=%d, want%d", count, want)
			}
		})
	}
}

func concurrentObjectSaves(t *testing.T, session *mcp.ClientSession, inputs []objectSaveInput) []objectResult {
	t.Helper()
	var group sync.WaitGroup
	results := make([]objectResult, len(inputs))
	for index, input := range inputs {
		group.Go(func() { results[index] = saveObjectForTest(t, session, input) })
	}
	group.Wait()
	return results
}

func TestObjectSaveRejectsInvalidComparisonAndBody(t *testing.T) {
	_, session := objectFixture(t, ObjectConfig{})
	observation := readObjectForTest(t, session, "key")
	base := saveInputForTest("key", "x", observation.ObservationToken, "request")
	cases := []objectSaveInput{base, base, base, base}
	cases[0].ObservationToken = ""
	cases[1].Replace = true
	cases[2].DataBase64 = "not-base64!"
	cases[3].RequestKey = ""
	for _, input := range cases {
		value := saveObjectForTest(t, session, input)
		if value.Outcome != "unknown" || value.Error == nil || value.Error.Code != "invalid" {
			t.Fatalf("invalid input=%+v", value)
		}
	}
	invalid := base
	invalid.ObservationToken = "unknown"
	if value := saveObjectForTest(t, session, invalid); value.Error == nil || value.Error.Code != "invalid_observation" {
		t.Fatalf("unknown token=%+v", value)
	}
	confirm := readObjectForTest(t, session, "key")
	if !confirm.Absent {
		t.Fatal("invalid requests published bytes")
	}
}
