package s3api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/capacity"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
)

// The error table is the only place a refusal from below becomes a status a client
// reads. A refusal that falls through it is reported as a server fault, and a 500 is
// a status a client retries: the caller is told to retry something that can never
// succeed. These three had no row.

func TestALiveWriteRefusalIsForbiddenAndNotAServerFault(t *testing.T) {
	got := mapStorageError(runthrough.ErrLiveWritesDisabled, "bucket/key")

	if got.StatusCode != http.StatusForbidden {
		t.Errorf("status is %d, want 403: the request is well-formed and the caller is authenticated, and the environment declined to propagate; 500 blames this server and invites a retry", got.StatusCode)
	}
	if got.Code != "AccessDenied" {
		t.Errorf("code is %q, want AccessDenied: an SDK branches on the code, and the authority refusal already answers this way", got.Code)
	}
	// The sentinel's own text names the configuration knob, because a developer
	// reading a server log needs it. The wire message must not: a caller learns
	// what it may not do without learning what this environment is configured
	// with, which is the rule the authority refusal states.
	for _, leaked := range []string{"allowLiveWrites", "STOW_"} {
		if strings.Contains(got.Message, leaked) {
			t.Errorf("message %q names the environment variable %q, so a caller learns the server's configuration", got.Message, leaked)
		}
	}
}

func TestAResourceUnresolvedNameIsNoSuchUploadAndNotAServerFault(t *testing.T) {
	got := mapStorageError(runtime.ErrResourceUnresolved, "bucket/key")

	if got.StatusCode != http.StatusNotFound {
		t.Errorf("status is %d, want 404: the operation addressed a handle this runtime cannot resolve, which is an absent upload rather than a broken server", got.StatusCode)
	}
	if got.Code != "NoSuchUpload" {
		t.Errorf("code is %q, want NoSuchUpload, matching what an unresolvable upload already answers through storage.ErrUploadNotFound", got.Code)
	}
}

func TestANamespaceCapacityRefusalIsInsufficientStorageAndNotAServerFault(t *testing.T) {
	got := mapStorageError(capacity.ErrFull, "bucket/key")

	if got.StatusCode != http.StatusInsufficientStorage {
		t.Errorf("status is %d, want 507: the runtime's own quota refusal already answers 507, and a store bound to a capacity namespace decides the same limit", got.StatusCode)
	}
	if got.Code != "InsufficientStorage" {
		t.Errorf("code is %q, want InsufficientStorage", got.Code)
	}
}

// storage.ErrInvalidUpload stays unmapped on purpose, and the workspace-owner wire
// test pins why: a corrupt or truncated stored upload record must answer 500, because
// it is a server-side integrity problem rather than a client error. A malformed
// upload ID does not reach that sentinel - the stores answer ErrUploadNotFound for one
// they did not mint, which is what S3 returns and what already maps to 404.

// The mappings match through errors.Is, so a refusal that arrives wrapped still
// answers as a refusal. Today these sentinels are returned bare; the test holds the
// property rather than the current call shape.
func TestWrappedRefusalsStillMap(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want int
	}{
		{"live write refusal", fmt.Errorf("put object: %w", runthrough.ErrLiveWritesDisabled), http.StatusForbidden},
		{"unresolved resource", fmt.Errorf("complete upload: %w", runtime.ErrResourceUnresolved), http.StatusNotFound},
		{"namespace capacity", fmt.Errorf("put object: %w", capacity.ErrFull), http.StatusInsufficientStorage},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := mapStorageError(testCase.err, "bucket/key")
			if got.StatusCode != testCase.want {
				t.Errorf("status is %d, want %d", got.StatusCode, testCase.want)
			}
			if got.StatusCode == http.StatusInternalServerError {
				t.Error("a refusal was reported as a server fault, so the client is invited to retry it")
			}
		})
	}
}

// A refusal must stay distinguishable from a genuine internal failure: the table
// still has to answer an unrecognised error as a 500, or the rows would have been
// replaced by a blanket success.
func TestAnUnrecognisedErrorIsStillAServerFault(t *testing.T) {
	got := mapStorageError(errors.New("something nobody mapped"), "bucket/key")
	if got.StatusCode != http.StatusInternalServerError {
		t.Errorf("status is %d, want 500 for an unmapped error", got.StatusCode)
	}
}
