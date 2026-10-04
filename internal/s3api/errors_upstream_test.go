package s3api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

// A dead upstream answered 500 InternalError "internal storage error": it blamed this
// server for a dependency that was not there, and 500 is not a status a client retries.

func upstreamUnreachable() error {
	return &runthrough.UpstreamError{
		Err:   errors.New("dial tcp 127.0.0.1:9000: connect: connection refused"),
		Class: runthrough.RetryClassTransient,
	}
}

func TestCommittedFailureRequiresVerificationBeforeRetry(t *testing.T) {
	got := mapStorageError(storage.CommittedError(storage.ErrObjectNotFound), "bucket/key")
	if got.StatusCode != http.StatusInternalServerError || got.Code != "InternalError" {
		t.Fatalf("committed failure mapped to underlying refusal: %+v", got)
	}
	if strings.Contains(got.Message, "retry is safe") || !strings.Contains(got.Message, "verify") {
		t.Fatalf("committed failure encouraged unverified retry: %q", got.Message)
	}
}

func TestAnUnreachableUpstreamIsTemporaryAndNotAServerFault(t *testing.T) {
	got := mapStorageError(upstreamUnreachable(), "models/bert.bin")

	if got.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status is %d, want 503: the upstream is a dependency that did not answer, and 500 blames this server for it", got.StatusCode)
	}
	if got.Code != "ServiceUnavailable" {
		t.Errorf("code is %q, want ServiceUnavailable: a client branches on the code and 500 invites the wrong conclusion", got.Code)
	}
	if got.Message == "" {
		t.Fatal("the message is empty, so an operator reading a log learns nothing")
	}
	for _, unwanted := range []string{"internal storage error", "InternalError"} {
		if strings.Contains(got.Message, unwanted) {
			t.Errorf("the message %q blames this server's storage for an upstream it could not reach", got.Message)
		}
	}
	if !strings.Contains(got.Message, "upstream") {
		t.Errorf("the message %q does not say the upstream is what failed", got.Message)
	}
}

// A 5xx gets the same answer for the same reason: the thing that failed is not this
// server, and reporting 500 would make every upstream outage look like a stow one.
func TestAnUpstreamServerErrorIsNotReportedAsThisServersFault(t *testing.T) {
	for _, status := range []int{500, 502, 503, 504} {
		got := mapStorageError(&runthrough.UpstreamError{
			Err:        errors.New("upstream said no"),
			StatusCode: status,
		}, "models/bert.bin")

		if got.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("an upstream %d produced status %d, want 503: the upstream is what failed", status, got.StatusCode)
		}
		if !strings.Contains(got.Message, fmt.Sprint(status)) {
			t.Errorf("an upstream %d produced the message %q, which does not say what the upstream returned", status, got.Message)
		}
	}
}

// A 4xx is a real answer, and replacing it with a guess would override a decision
// somebody made on purpose.
func TestAnUpstreamRefusalIsPassedThroughRatherThanGuessedAt(t *testing.T) {
	got := mapStorageError(&runthrough.UpstreamError{
		Err:        errors.New("access denied"),
		StatusCode: http.StatusForbidden,
		Code:       "AccessDenied",
	}, "models/bert.bin")

	if got.StatusCode != http.StatusForbidden {
		t.Errorf("status is %d, want 403: the upstream refused, and its refusal is the answer", got.StatusCode)
	}
	if got.Code != "AccessDenied" {
		t.Errorf("code is %q, want the upstream's own AccessDenied rather than a guess", got.Code)
	}
}

func TestAnUpstreamRefusalWithoutACodeStillReportsTheRefusal(t *testing.T) {
	got := mapStorageError(&runthrough.UpstreamError{
		Err:        errors.New("upstream said no"),
		StatusCode: http.StatusForbidden,
	}, "models/bert.bin")

	if got.StatusCode != http.StatusForbidden {
		t.Errorf("status is %d, want 403", got.StatusCode)
	}
	if got.Code == "" {
		t.Error("the code is empty, so a client branching on it gets nothing")
	}
}

// The upstream branch inspects the error rather than matching a sentinel, so
// widening it to any unrecognised error is the obvious way to break the 500 that is
// left.
func TestAFaultInThisServerIsStillAnInternalError(t *testing.T) {
	got := mapStorageError(errors.New("a nil pointer in a handler"), "models/bert.bin")

	if got.StatusCode != http.StatusInternalServerError {
		t.Errorf("status is %d, want 500: an unclassified error is this server's own fault and nothing has changed that", got.StatusCode)
	}
	if got.Code != "InternalError" {
		t.Errorf("code is %q, want InternalError", got.Code)
	}
}

// The sentinel matches only the no-response case: a provider error carrying a status
// reached the upstream and got an answer, however unwelcome.
func TestTheUnreachableSentinelMatchesOnlyTheNoResponseCase(t *testing.T) {
	if !errors.Is(upstreamUnreachable(), runthrough.ErrUpstreamUnreachable) {
		t.Error("an upstream error with no status does not match ErrUpstreamUnreachable")
	}
	if errors.Is(&runthrough.UpstreamError{StatusCode: 503}, runthrough.ErrUpstreamUnreachable) {
		t.Error("an upstream error carrying a 503 matches ErrUpstreamUnreachable, and that upstream did answer")
	}
	if errors.Is(&runthrough.UpstreamError{StatusCode: 403}, runthrough.ErrUpstreamUnreachable) {
		t.Error("an upstream refusal matches ErrUpstreamUnreachable, and it is a decision rather than a connection failure")
	}
	if errors.Is(errors.New("something else entirely"), runthrough.ErrUpstreamUnreachable) {
		t.Error("an unrelated error matches ErrUpstreamUnreachable")
	}
}

func TestTheUpstreamMappingDoesNotShadowTheStorageTable(t *testing.T) {
	got := mapStorageError(storage.ErrObjectNotFound, "models/bert.bin")

	if got.StatusCode != http.StatusNotFound || got.Code != "NoSuchKey" {
		t.Errorf("a missing key produced %d %s, want 404 NoSuchKey", got.StatusCode, got.Code)
	}
}
