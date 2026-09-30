package mcpstorage

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestObjectManagedSaveAndRetainedReplay(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{})
	read := readObjectForTest(t, session, "report")
	if read.Error != nil || !read.Available || !read.Absent || read.ObservationToken == "" {
		t.Fatalf("read absence=%+v", read)
	}
	input := saveInputForTest("report", "first", read.ObservationToken, "request-first")
	input.Metadata = map[string]string{"state": "first"}
	first := saveObjectForTest(t, session, input)
	requireObjectCommitted(t, first)
	replay := saveObjectForTest(t, session, input)
	requireObjectCommitted(t, replay)
	if !replay.Replayed || replay.Object.LastModified != first.Object.LastModified {
		t.Fatalf("live replay=%+v", replay)
	}
	read = readObjectForTest(t, session, "report")
	if read.Object == nil || read.Object.DataBase64 != base64.StdEncoding.EncodeToString([]byte("first")) {
		t.Fatalf("read data=%+v", read)
	}
	newer := saveObjectForTest(t, session, saveInputForTest("report", "newer", read.ObservationToken, "request-newer"))
	requireObjectCommitted(t, newer)
	old := invokeObject(t, session, "stow_object_resolve_save", objectResolveInput{Key: "report", RequestKey: "request-first"})
	requireObjectCommitted(t, old)
	if !old.Replayed || old.Object.Metadata["state"] != "first" {
		t.Fatalf("original receipt=%+v", old)
	}
	changed := input
	changed.DataBase64 = base64.StdEncoding.EncodeToString([]byte("different"))
	if value := saveObjectForTest(t, session, changed); value.Error == nil || value.Error.Code != "request_conflict" {
		t.Fatalf("changed meaning=%+v", value)
	}
	confirm := readObjectForTest(t, session, "report")
	if confirm.Object.DataBase64 != base64.StdEncoding.EncodeToString([]byte("newer")) {
		t.Fatal("replay overwrote newer data")
	}
	assertObjectReceiptAfterDelete(t, host, session, first)
}

func assertObjectReceiptAfterDelete(t *testing.T, host *ObjectServer, session *mcp.ClientSession, first objectResult) {
	t.Helper()
	if err := host.runtime.DeleteObject(context.Background(), host.config.Bucket, "report"); err != nil {
		t.Fatal(err)
	}
	deleted := invokeObject(t, session, "stow_object_resolve_save", objectResolveInput{Key: "report", RequestKey: "request-first"})
	requireObjectCommitted(t, deleted)
	if deleted.Object.LastModified != first.Object.LastModified {
		t.Fatal("deleted target lost original receipt")
	}
}

func TestObjectStaleMetadataAndABARefuse(t *testing.T) {
	for _, change := range []string{"metadata", "ABA"} {
		t.Run(change, func(t *testing.T) {
			host, session := objectFixture(t, ObjectConfig{})
			if _, err := host.runtime.PutObject(context.Background(), host.config.Bucket, "report", []byte("A"), stow.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			observation := readObjectForTest(t, session, "report")
			if change == "ABA" {
				if _, err := host.runtime.PutObject(context.Background(), host.config.Bucket, "report", []byte("B"), stow.PutOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			options := stow.PutOptions{}
			if change == "metadata" {
				options.Metadata = map[string]string{"reviewed": "yes"}
			}
			if _, err := host.runtime.PutObject(context.Background(), host.config.Bucket, "report", []byte("A"), options); err != nil {
				t.Fatal(err)
			}
			stale := saveObjectForTest(t, session, saveInputForTest("report", "stale", observation.ObservationToken, "stale-request"))
			if stale.Outcome != "not_committed" || stale.Error == nil || stale.Error.Code != "conflict" {
				t.Fatalf("stale=%+v", stale)
			}
			current := readObjectForTest(t, session, "report")
			if current.Object.Metadata["reviewed"] != options.Metadata["reviewed"] {
				t.Fatal("stale save altered properties")
			}
		})
	}
}

func TestObjectExplicitReplacementAndRestartResolve(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{})
	input := saveInputForTest("report", "first", "", "request")
	input.Replace = true
	first := saveObjectForTest(t, session, input)
	requireObjectCommitted(t, first)
	oldObservation := readObjectForTest(t, session, "report")
	config := host.config
	config.CreateBucket = false
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, restarted := objectFixture(t, config)
	oldRetry := saveObjectForTest(t, restarted, saveInputForTest("report", "first", oldObservation.ObservationToken, "request"))
	if oldRetry.Outcome != "unknown" || oldRetry.Error == nil || oldRetry.Error.Code != "invalid_observation" {
		t.Fatalf("old host token=%+v", oldRetry)
	}
	resolved := invokeObject(t, restarted, "stow_object_resolve_save", objectResolveInput{Key: "report", RequestKey: "request"})
	requireObjectCommitted(t, resolved)
	if !resolved.Replayed || resolved.Object.LastModified != first.Object.LastModified {
		t.Fatalf("restart original result=%+v", resolved)
	}
	unknown := invokeObject(t, restarted, "stow_object_resolve_save", objectResolveInput{Key: "report", RequestKey: "missing"})
	if unknown.Outcome != "unknown" || unknown.Error == nil || unknown.Error.Code != "not_found" {
		t.Fatalf("missing receipt=%+v", unknown)
	}
	if _, err := reopened.runtime.GetObject(context.Background(), config.Bucket, "report"); err != nil {
		t.Fatal(err)
	}
}

func TestObjectReadOnlyUsesCurrentAuthority(t *testing.T) {
	host, session := objectFixture(t, ObjectConfig{})
	input := saveInputForTest("report", "saved", "", "request")
	input.Replace = true
	requireObjectCommitted(t, saveObjectForTest(t, session, input))
	config := host.config
	config.CreateBucket = false
	config.ReadOnly = true
	_ = session.Close()
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	_, readonly := objectFixture(t, config)
	read := readObjectForTest(t, readonly, "report")
	if read.Error != nil {
		t.Fatalf("read-only read=%+v", read)
	}
	denied := saveObjectForTest(t, readonly, saveInputForTest("report", "changed", read.ObservationToken, "denied-request"))
	if denied.Outcome != "unknown" || denied.Error == nil || denied.Error.Code != "denied" {
		t.Fatalf("denied save=%+v", denied)
	}
	requireObjectCommitted(t, invokeObject(t, readonly, "stow_object_resolve_save", objectResolveInput{Key: "report", RequestKey: "request"}))
}
