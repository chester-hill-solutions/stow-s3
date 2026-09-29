package mcpstorage

import (
	"encoding/json"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"os"
	"path/filepath"
	"testing"
)

func TestBundleDocumentRejectsScopeExpansion(t *testing.T) {
	for _, change := range []func(*stow.Handoff){
		func(d *stow.Handoff) { d.Team = "outside" },
		func(d *stow.Handoff) { d.Archive.Path = "../outside.tar.gz" },
		func(d *stow.Handoff) { d.Archive.Path = "/outside.tar.gz" },
	} {
		document := stow.Handoff{Version: 2, WorkspaceID: "source", Archive: &stow.HandoffArchive{Path: "checkpoint.tar.gz", SHA256: "digest"}}
		change(&document)
		path := filepath.Join(t.TempDir(), "handoff.json")
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = readBundleDocument(file)
		file.Close()
		if err == nil {
			t.Fatalf("accepted %+v", document)
		}
	}
}

func TestAdoptionRefusesSymlinkedBundleFile(t *testing.T) {
	_, _, config := fixture(t)
	config.ExportRoot, config.AdoptRoot = t.TempDir(), t.TempDir()
	bundle := filepath.Join(config.ExportRoot, "bundle")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "handoff.json")
	if err := os.WriteFile(outside, []byte(`{"version":2,"workspace_id":"source","archive":{"path":"checkpoint.tar.gz","sha256":"digest"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(bundle, "handoff.json")); err != nil {
		t.Fatal(err)
	}
	a := &adapter{config: config}
	_, out, err := a.adopt(t.Context(), nil, adoptInput{Bundle: "bundle", Destination: "new"})
	if err != nil || out.Error == nil {
		t.Fatalf("accepted link: %+v %v", out, err)
	}
}

func TestExportUncertainOutcomeKeepsErrorAndBundleReceipt(t *testing.T) {
	document := &stow.Handoff{Version: 2, WorkspaceID: "source", CheckpointID: "saved"}
	response, value, err := finish(result{Outcome: "committed", Bundle: "bundle", Handoff: document}, &stow.HandoffError{BundleDir: "/transfer/bundle", Outcome: "unknown", Err: os.ErrPermission})
	if err != nil || !response.IsError || value.Outcome != "unknown" {
		t.Fatalf("uncertainty lost: %+v %v", value, err)
	}
	if value.Handoff != document || value.Bundle != "bundle" || value.Error.Retryable {
		t.Fatalf("bad receipt: %+v", value)
	}
}

func TestPartialImportPreservesIsError(t *testing.T) {
	_, failed, _ := finish(result{OriginCheckpointID: "imported"}, os.ErrPermission)
	failed.Outcome = "partial_import"
	response, value, err := finish(failed, nil)
	if err != nil || !response.IsError || value.Error == nil || value.Outcome != "partial_import" {
		t.Fatalf("partial import error lost: %+v %v", value, err)
	}
}
