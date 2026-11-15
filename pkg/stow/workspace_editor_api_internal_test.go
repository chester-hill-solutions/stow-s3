package stow

import (
	"errors"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestWorkspaceFileSavePublicErrorMapping(t *testing.T) {
	if got := mapWorkspaceFileSaveError(storage.ErrWorkspaceFileConflict); !errors.Is(got, ErrSaveConflict) {
		t.Fatalf("basis conflict mapping = %v", got)
	}
	if got := mapWorkspaceFileSaveError(runtime.ErrQuotaExceeded); !errors.Is(got, ErrQuotaExceeded) {
		t.Fatalf("quota mapping = %v", got)
	}
	denied := authority.ReadOnly().Check(authority.ObjectWrite)
	if got := mapWorkspaceFileSaveError(denied); !errors.Is(got, denied) {
		t.Fatalf("authority error mapping = %v", got)
	}
	filesystem := errors.New("/private/workspace/.stow/index.json: permission denied")
	committed := mapWorkspaceFileSaveError(storage.CommittedError(filesystem))
	if !errors.Is(committed, ErrMutationCommitted) || !errors.Is(committed, ErrWorkspaceEditorUnavailable) || strings.Contains(committed.Error(), "/private/workspace") {
		t.Fatalf("committed cleanup error mapping = %v", committed)
	}
}
