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

// A run-through server whose upstream has gone away used to answer every read with
// InternalError and "internal storage error".
//
// That is the wrong answer in three ways at once, and it was the wrong answer in the
// one situation this product exists to be running when. 500 says the server is
// broken; the server is fine, its dependency is not. 500 is not the status a client
// retries. And "internal storage error" names stow's own storage layer, so a human
// reading a log concluded the workspace was corrupt when what had happened was that
// the network went away — which is the event the cache exists for.
//
// These tests pin the replacement and, just as importantly, the boundary around it:
// a dead upstream with a warm cache must still serve the cached copy, because a fix
// that returned 503 in that case would have broken the actual product.

func upstreamUnreachable() error {
	return &runthrough.UpstreamError{
		Err:   errors.New("dial tcp 127.0.0.1:9000: connect: connection refused"),
		Class: runthrough.RetryClassTransient,
	}
}

// The headline case: asked and never answered.
func TestAnUnreachableUpstreamIsTemporaryAndNotAServerFault(t *testing.T) {
	got := mapStorageError(upstreamUnreachable(), "models/bert.bin")

	if got.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status is %d, want 503: the upstream is a dependency that did not answer, and 500 blames this server for it", got.StatusCode)
	}
	if got.Code != "ServiceUnavailable" {
		t.Errorf("code is %q, want ServiceUnavailable: a client branches on the code and 500 invites the wrong conclusion", got.Code)
	}
	// The message is the part a human reads. It has to name the situation rather than
	// stow's internals, and it has to say the request is worth repeating.
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

// A 5xx from the upstream is the same class of event with a different cause, and it
// gets the same answer for the same reason: the thing that failed is not this
// server. Reporting 500 here would mean every upstream outage looked like a stow
// outage, which is exactly the misattribution that makes an incident hard to start.
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

// A 4xx is not a failure of this server or of the connection. The upstream gave a
// real answer, and replacing it with a guess would override a decision somebody made
// on purpose — an AccessDenied from the upstream means the credentials are wrong, and
// InternalError means nothing useful at all.
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

// An upstream that refuses without naming a reason still has to be reported as a
// refusal, so the status is the load-bearing part and a code has to be supplied.
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

// A genuine fault in this server must still say so. The upstream mapping is keyed on
// the error being a provider error, and widening it to "any unrecognised error" is
// the obvious way to break the 500 that is left — so a plain internal failure is
// checked explicitly.
func TestAFaultInThisServerIsStillAnInternalError(t *testing.T) {
	got := mapStorageError(errors.New("a nil pointer in a handler"), "models/bert.bin")

	if got.StatusCode != http.StatusInternalServerError {
		t.Errorf("status is %d, want 500: an unclassified error is this server's own fault and nothing has changed that", got.StatusCode)
	}
	if got.Code != "InternalError" {
		t.Errorf("code is %q, want InternalError", got.Code)
	}
}

// The sentinel is how a consumer recognises the condition without importing the
// concrete type, so it has to match exactly the case where no response arrived. A
// provider error carrying a status reached the upstream and got an answer, however
// unwelcome — and that is not the same condition.
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

// The mapping must not shadow the storage table. A NoSuchKey from the local store is
// still a NoSuchKey, and the upstream branch is reached first precisely because it
// inspects the error rather than matching a sentinel — so an error that is both has
// to come out the way the table says.
func TestTheUpstreamMappingDoesNotShadowTheStorageTable(t *testing.T) {
	got := mapStorageError(storage.ErrObjectNotFound, "models/bert.bin")

	if got.StatusCode != http.StatusNotFound || got.Code != "NoSuchKey" {
		t.Errorf("a missing key produced %d %s, want 404 NoSuchKey", got.StatusCode, got.Code)
	}
}
