package stow_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceEditorHonoursAConfiguredBoundAboveSixtyFourKiB(t *testing.T) {
	ws, editor, _ := openWorkspaceEditorFixture(t, stow.WorkspaceEditOptions{
		ReadRoots: []string{"docs"}, WriteRoots: []string{"docs"}, Coordination: "host_quiesced", MaxFileBytes: 262144,
	})
	defer ws.Close()
	payload := bytes.Repeat([]byte("a"), 100<<10)
	path := filepath.Join(ws.Dir(), "docs", "large.bin")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	read, err := editor.ReadForSave(context.Background(), "docs/large.bin")
	if err != nil {
		t.Fatalf("read above 64 KiB: %v", err)
	}
	saved, err := editor.SaveFile(context.Background(), stow.WorkspaceFileSaveRequest{
		Path: "docs/large.bin", Data: payload, ObservationToken: read.ObservationToken, RequestKey: "large-save",
	})
	if err != nil {
		t.Fatalf("save above 64 KiB: %v", err)
	}
	if saved.Outcome != stow.SaveCommitted {
		t.Fatalf("save outcome = %v", saved.Outcome)
	}
}
