package mcpstorage

import (
	"fmt"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceFileRefusalsReportNotCommittedAndNameTheNextCall(t *testing.T) {
	_, refused, err := finishWorkspaceFileTool(workspaceFileToolResult{}, stow.ErrWorkspaceObservationInvalid)
	if err != nil {
		t.Fatal(err)
	}
	if refused.Outcome != string(stow.SaveNotCommitted) || refused.Next == "" {
		t.Fatalf("refusal = outcome %q next %q", refused.Outcome, refused.Next)
	}

	_, unknown, err := finishWorkspaceFileTool(workspaceFileToolResult{}, fmt.Errorf("%w: publish", stow.ErrSaveRequestUnknown))
	if err != nil {
		t.Fatal(err)
	}
	if unknown.Outcome != string(stow.SaveUnknown) || unknown.Next == "" {
		t.Fatalf("unknown = outcome %q next %q", unknown.Outcome, unknown.Next)
	}

	_, committed, err := finishWorkspaceFileTool(workspaceFileToolResult{}, fmt.Errorf("%w: index", stow.ErrMutationCommitted))
	if err != nil {
		t.Fatal(err)
	}
	if committed.Outcome != string(stow.SaveCommitted) || committed.Next == "" || !committed.Committed {
		t.Fatalf("committed = outcome %q next %q committed %v", committed.Outcome, committed.Next, committed.Committed)
	}
}
