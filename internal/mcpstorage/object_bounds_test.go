package mcpstorage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestObjectObservationLifecycleAndBounds(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{MaxObservations: 1})
	first := readObjectForTest(t, session, "first")
	full := readObjectForTest(t, session, "second")
	if full.Error == nil || full.Error.Code != "observation_full" || len(host.observations) != 1 {
		t.Fatalf("pool=%+v", full)
	}
	wrong := saveObjectForTest(t, session, saveInputForTest("other", "x", first.ObservationToken, "wrong"))
	if wrong.Outcome != "unknown" || wrong.Error == nil || wrong.Error.Code != "invalid_observation" {
		t.Fatalf("wrong resource=%+v", wrong)
	}
	release := objectReleaseInput{ObservationToken: first.ObservationToken}
	if value := invokeObject(t, session, "stow_object_release_observation", release); !value.Released {
		t.Fatalf("release=%+v", value)
	}
	invokeObject(t, session, "stow_object_release_observation", release)
	value := saveObjectForTest(t, session, saveInputForTest("first", "x", first.ObservationToken, "released"))
	if value.Error == nil || value.Error.Code != "invalid_observation" {
		t.Fatalf("released=%+v", value)
	}
	second := readObjectForTest(t, session, "second")
	host.mu.Lock()
	future := host.now().Add(observationTTL)
	host.now = func() time.Time { return future }
	host.mu.Unlock()
	expired := saveObjectForTest(t, session, saveInputForTest("second", "x", second.ObservationToken, "expired"))
	if expired.Error == nil || expired.Error.Code != "expired_observation" {
		t.Fatalf("expired=%+v", expired)
	}
	if value := readObjectForTest(t, session, "third"); value.Error != nil {
		t.Fatalf("expired slot not reaped=%+v", value)
	}
}

func TestObjectPayloadAndResponseLimitsReleaseObservation(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{})
	if _, err := host.runtime.PutObject(context.Background(), host.config.Bucket, "too-big", make([]byte, maxObjectPayload+1), stow.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if value := readObjectForTest(t, session, "too-big"); value.Error == nil || value.Error.Code != "payload_too_large" || len(host.observations) != 0 {
		t.Fatalf("large read=%+v", value)
	}
	input := saveInputForTest("new", strings.Repeat("x", maxObjectPayload+1), "", "large")
	input.Replace = true
	if value := saveObjectForTest(t, session, input); value.Outcome != "unknown" || value.Error == nil || value.Error.Code != "payload_too_large" {
		t.Fatalf("large save=%+v", value)
	}
	body := make([]byte, maxObjectPayload)
	if _, err := host.runtime.PutObject(context.Background(), host.config.Bucket, "large-response", body, stow.PutOptions{Metadata: map[string]string{"data": strings.Repeat("x", 60<<10)}}); err != nil {
		t.Fatal(err)
	}
	value := readObjectForTest(t, session, "large-response")
	if value.Error == nil || value.Error.Code != "response_too_large" || value.ObservationToken != "" || len(host.observations) != 0 {
		t.Fatalf("response limit=%+v, observations=%d", value, len(host.observations))
	}
}

func TestObjectEncodingFailurePreservesEffect(t *testing.T) {
	for _, outcome := range []string{"committed", "unknown"} {
		value := objectResult{Outcome: outcome, Available: true, Replayed: true, Object: &objectView{Key: "key", LastModified: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}
		response, encoded, err := finishObject(value, nil)
		if err != nil || response == nil || encoded.Outcome != outcome || !encoded.Replayed || encoded.Error == nil || encoded.Error.Code != "response_encoding" {
			t.Fatalf("encoding failure=%+v,%v", encoded, err)
		}
		large := objectResult{Outcome: outcome, Replayed: true, Object: &objectView{Metadata: map[string]string{"large": strings.Repeat("x", maxResponseBytes)}}}
		_, encoded, err = finishObject(large, nil)
		if err != nil || encoded.Outcome != outcome || !encoded.Replayed || encoded.Available {
			t.Fatalf("oversize effect=%+v,%v", encoded, err)
		}
	}
}

func TestObjectSDKValidationResponseRemainsBounded(t *testing.T) {
	_, session := objectFixture(t, ObjectConfig{})
	for _, test := range []struct{ name, code string }{{strings.Repeat("x", 300<<10), "payload_too_large"}, {strings.Repeat("\n", 90<<10), "response_too_large"}} {
		input := map[string]string{test.name: "extra"}
		encoded, _ := json.Marshal(input)
		if test.code == "response_too_large" && len(encoded) > maxResponseBytes {
			t.Fatal("schema fixture exceeded input budget")
		}
		value := invokeObject(t, session, "stow_object_capabilities", input)
		if value.Outcome != "unknown" || value.Error == nil || value.Error.Code != test.code {
			t.Fatalf("oversized validation=%+v", value)
		}
	}
}

func TestObjectCoreErrorPreservesOutcome(t *testing.T) {
	for _, outcome := range []stow.SaveOutcome{stow.SaveCommitted, stow.SaveUnknown} {
		response, value, err := finishObject(savedObjectResult(stow.SaveResult{Outcome: outcome, Replayed: true}), context.Canceled)
		if err != nil || !response.IsError || value.Outcome != string(outcome) || !value.Replayed || value.Error.Code != "canceled" {
			t.Fatalf("core error=%+v,%v", value, err)
		}
	}
}

func TestObjectConcurrentCallAdmissionPreservesNegotiation(t *testing.T) {
	host := &ObjectServer{}
	started := make(chan struct{}, 8)
	release := make(chan struct{})
	next := func(_ context.Context, method string, _ mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return &mcp.CallToolResult{}, nil
		}
		started <- struct{}{}
		<-release
		return &mcp.CallToolResult{}, nil
	}
	handle := host.limitCalls(next)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() { _, _ = handle(context.Background(), "tools/call", nil) })
	}
	for range 8 {
		<-started
	}
	if _, err := handle(context.Background(), "initialize", nil); err != nil {
		t.Fatal(err)
	}
	busy, err := handle(context.Background(), "tools/call", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(busy.(*mcp.CallToolResult).StructuredContent)
	var result objectResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.Error == nil || result.Error.Code != "busy" || result.Outcome != "unknown" {
		t.Fatalf("busy=%+v", result)
	}
	close(release)
	group.Wait()
}

func TestObjectConfigRejectsBeforeOpening(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "unopened")
	for _, config := range []ObjectConfig{
		{Dir: dir, Bucket: "objects", ReadOnly: true, CreateBucket: true},
		{Dir: dir, Bucket: "objects", MaxObjectBytes: maxObjectPayload + 1},
		{Dir: dir, Bucket: "objects", MaxObservations: maxObjectObservations + 1},
		{Dir: dir, Bucket: "objects", MaxBytes: -1},
		{Dir: dir, Bucket: "objects", MaxObjects: -1},
		{Dir: dir, Bucket: "objects", MaxObjectBytes: -1},
		{Dir: dir, Bucket: "objects", MaxObservations: -1},
	} {
		if host, err := NewObjects(config); err == nil {
			_ = host.Close()
			t.Fatalf("invalid config=%+v", config)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid config opened storage")
	}
}

func TestObjectCancellationAndClosedLifecycle(t *testing.T) {
	host, _ := objectFixture(t, ObjectConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, value, err := host.saveObject(ctx, nil, objectSaveInput{Key: "new", Replace: true, RequestKey: "request"})
	if err != nil || value.Outcome != "unknown" || value.Error == nil || value.Error.Code != "canceled" {
		t.Fatalf("canceled=%+v,%v", value, err)
	}
	host.mu.Lock()
	_, _ = host.retainObservation("key", stow.SaveCondition{})
	host.mu.Unlock()
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if len(host.observations) != 0 {
		t.Fatal("close retained volatile conditions")
	}
	_, value, err = host.resolveObject(context.Background(), nil, objectResolveInput{Key: "new", RequestKey: "request"})
	if err != nil || value.Outcome != "unknown" || value.Error == nil || value.Error.Code != "closed" {
		t.Fatalf("closed=%+v,%v", value, err)
	}
}
