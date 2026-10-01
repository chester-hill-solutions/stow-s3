package auth_test

// SigV4 strictness: the verifier refuses authentication material it cannot read
// exactly as the client sent it, rather than repairing it into something it can.
//
// Every case here was a way to get a request authenticated against a statement
// the client did not make. Repairing malformed material is not leniency — it is
// the verifier substituting its own reading of what was signed, and everything
// downstream then trusts that reading.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

var strictCreds = auth.Credentials{
	AccessKeyID:     "AKIAEXAMPLE",
	SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
}

// presignedAt signs a presigned URL at signingTime and returns it, so a case can
// place the request on either side of the reference time without touching the
// verifier's clock.
func presignedAt(t *testing.T, signingTime time.Time, expires int) string {
	t.Helper()
	return signPresignedURL(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", strictCreds, "us-east-1", signingTime, expires, nil)
}

// A presigned URL is not valid arbitrarily far before its signing time.
//
// The signing time is inside the URL, so it is the client's to choose, and the
// expiry check alone could not notice a wrong one: a URL signed for a date next
// year has an expiry next year, so "now is after the expiry" is false and the
// URL authenticates for as long as the client asked. An ordinary signed request
// does not have that freedom, because its date is bound by the signature and
// checked for skew in both directions.
//
// Every case below is decided by a fixed reference time, so none of them depends
// on how fast the machine runs.
func TestPresignedURLIsNotValidBeforeItsSigningTime(t *testing.T) {
	const allowedSkew = 15 * time.Minute
	reference := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	verifier := &auth.Verifier{Region: "us-east-1", MaxSkew: allowedSkew}

	// Every case is given an expiry that puts the decision where the case is
	// about: a future-dated URL with a long expiry, so the expiry check cannot be
	// what refuses it, and an expired one with a signing time at the reference
	// point, so the skew check cannot be.
	cases := []struct {
		name     string
		relative time.Duration
		expires  int
		wantCode string
	}{
		// A signing time outside the allowance ahead of the server's clock. The
		// URL has not expired — it expires an hour later still — so under the old
		// check this authenticated, and would have kept doing so for the whole
		// hour the client asked for.
		{"signed an hour from now", time.Hour, 3600, "RequestTimeTooSkewed"},
		{"signed a day from now", 24 * time.Hour, 604800, "RequestTimeTooSkewed"},
		{"signed a year from now", 365 * 24 * time.Hour, 604800, "RequestTimeTooSkewed"},
		// Just inside the allowance: still valid, because the allowance is the
		// whole point of having one.
		{"signed one second from now", time.Second, 3600, ""},
		{"signed exactly the allowed skew from now", allowedSkew, 3600, ""},
		// The ordinary window: signed at or before the server's clock, used now.
		{"signed at the reference time", 0, 3600, ""},
		{"signed a minute ago", -time.Minute, 3600, ""},
		{"signed an hour ago, within its expiry", -time.Hour, 7200, ""},
		// Past its own expiry, which is the check that already worked. The signing
		// time is two hours back and the expiry one hour after it, so the URL's
		// own window has closed and the skew check has nothing to say.
		{"past its expiry", -2 * time.Hour, 3600, "AccessDenied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqURL := presignedAt(t, reference.Add(tc.relative), tc.expires)
			req := httptestRequest(t, http.MethodGet, reqURL, nil)
			req.Header.Set("Host", "127.0.0.1:9000")

			err := verifier.AuthenticateAt(req, strictCreds, reference)
			switch {
			case tc.wantCode == "" && err != nil:
				t.Fatalf("a URL %s was refused: %v", tc.relative, err)
			case tc.wantCode == "":
				return
			}
			assertAuthCode(t, err, tc.wantCode)
		})
	}
}

// The allowance is the verifier's, not a constant.
//
// A verifier configured with a tight allowance refuses a URL signed slightly ahead
// of the server's clock, and a permissive one accepts it. If the presigned branch
// used its own idea of the allowance, the configured value would govern signed
// requests and nothing else — which is the shape of the bug being fixed, one
// branch narrower.
func TestPresignedSkewUsesTheConfiguredAllowance(t *testing.T) {
	reference := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	// Half an hour ahead of the server's clock: outside a five-minute allowance,
	// inside an hour's.
	reqURL := presignedAt(t, reference.Add(30*time.Minute), 3600)
	req := httptestRequest(t, http.MethodGet, reqURL, nil)
	req.Header.Set("Host", "127.0.0.1:9000")

	if err := (&auth.Verifier{Region: "us-east-1", MaxSkew: 5 * time.Minute}).AuthenticateAt(req, strictCreds, reference); err == nil {
		t.Fatal("a verifier with a 5-minute allowance accepted a URL signed 30 minutes ahead")
	}
	if err := (&auth.Verifier{Region: "us-east-1", MaxSkew: time.Hour}).AuthenticateAt(req, strictCreds, reference); err != nil {
		t.Fatalf("a verifier with a one-hour allowance refused a URL signed 30 minutes ahead: %v", err)
	}
}

// An expiration that is malformed, negative or beyond the ceiling is refused.
//
// The bound is S3's seven days, and it is the only thing standing between a
// presigned URL and a permanent one — so a value that is not a number, or is a
// number outside the range, has to be refused rather than clamped or defaulted.
func TestPresignedExpiryValuesAreRefusedWhenUnusable(t *testing.T) {
	reference := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	verifier := &auth.Verifier{Region: "us-east-1", MaxSkew: 15 * time.Minute}
	valid := signPresignedURL(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", strictCreds, "us-east-1", reference, 3600, nil)

	cases := []struct {
		name    string
		expires string
	}{
		{"not a number", "soon"},
		{"empty", ""},
		{"zero", "0"},
		{"negative", "-1"},
		{"one past the ceiling", "604801"},
		{"absurd", "99999999999"},
		{"fractional", "3600.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The signature was computed over a valid X-Amz-Expires, so changing
			// the value afterwards invalidates the signature too. That is fine:
			// what is being checked is that the request is refused, and refusing
			// for the expiry rather than for the signature is asserted by the
			// error code below.
			req := httptestRequest(t, http.MethodGet, withQueryParam(t, valid, "X-Amz-Expires", tc.expires), nil)
			req.Header.Set("Host", "127.0.0.1:9000")
			assertAuthCode(t, verifier.AuthenticateAt(req, strictCreds, reference), "AccessDenied")
		})
	}

	// And the boundary itself is legal, so the ceiling is a bound rather than an
	// off-by-one that refuses the largest legal URL.
	atCeiling := signPresignedURL(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", strictCreds, "us-east-1", reference, 604800, nil)
	req := httptestRequest(t, http.MethodGet, atCeiling, nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	if err := verifier.AuthenticateAt(req, strictCreds, reference); err != nil {
		t.Fatalf("a URL at the seven-day ceiling was refused: %v", err)
	}
}

// withQueryParam replaces one query parameter's value, leaving every other
// parameter — including the signature — alone.
func withQueryParam(t *testing.T, rawURL, key, value string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// appendQueryParam adds a parameter without disturbing the existing ones, which is
// how a duplicate or a differently-cased variant is introduced.
func appendQueryParam(t *testing.T, rawURL, key, value string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	query := parsed.Query()
	query.Add(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// A SignedHeaders list that cannot be read as the client's statement is refused.
//
// The list is embedded verbatim in the canonical request, so anything the
// verifier does to it — dropping an empty entry, folding case, sorting,
// deduplicating — changes which headers the signature covers while the request
// stays as it was. Every form below is a list whose text is not the text the
// signature was computed over.
func TestMalformedSignedHeadersAreRefused(t *testing.T) {
	creds := strictCreds
	now := time.Now().UTC().Truncate(time.Second)

	// Every case keeps the whole header set the request actually carries — host,
	// x-amz-date and x-amz-content-sha256 — so a verifier which repairs the list
	// ends up with one the client could legitimately have signed, and the refusal
	// can only come from the list itself. docs/CODE_STANDARDS.md explains why a
	// shorter list makes all of this vacuous.
	//
	// Verified by loosening the parser: the empty entries, both duplicates, both
	// upper-case spellings and both orderings fall without the rule. The four that
	// still pass do so for reasons of their own — a name that is not a field name
	// cannot be found, and a list without `host` is refused before signing.
	cases := []struct {
		name          string
		signedHeaders string
	}{
		// An empty entry: the entry the client wrote is the entry the signature
		// covered, and dropping it silently narrows the request.
		{"an empty entry", "host;x-amz-content-sha256;;x-amz-date"},
		{"a trailing separator", "host;x-amz-content-sha256;x-amz-date;"},
		{"a leading separator", ";host;x-amz-content-sha256;x-amz-date"},
		{"an empty entry in the middle", "host;;x-amz-content-sha256;x-amz-date"},
		// A repeated name: the list names one header twice.
		{"a duplicate of the last name", "host;x-amz-content-sha256;x-amz-date;x-amz-date"},
		{"a duplicate of the first name", "host;host;x-amz-content-sha256;x-amz-date"},
		// Case: the client's spelling is its own, and folding it is a rewrite
		// that produces exactly the list a conforming client would have sent.
		{"one upper-case name", "host;x-amz-content-sha256;X-Amz-Date"},
		{"an upper-case host", "Host;x-amz-content-sha256;x-amz-date"},
		// Ordering: the canonical request embeds the list in the order written, so
		// sorting it locally produces exactly the conforming list.
		{"one inversion in a longer list", "host;x-amz-date;x-amz-content-sha256"},
		{"fully reversed", "x-amz-date;x-amz-content-sha256;host"},
		// Structure: a name that is not a field name cannot be looked up on the
		// request at all, so a verifier that accepts it is reading a header the
		// request does not carry.
		{"a space inside a name", "host;x-amz-content-sha256;x-amz date"},
		{"a padded name", "host;x-amz-content-sha256; x-amz-date"},
		{"a colon", "host:x-amz-content-sha256;x-amz-date"},
		// No host at all: the canonical request would never name the header the
		// request is addressed by.
		{"no host", "x-amz-content-sha256;x-amz-date"},
		{"empty", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
			req.Header.Set("Host", "127.0.0.1:9000")
			req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
			req.Header.Set("X-Amz-Content-Sha256", unsignedPayloadForTest)
			// Signed over the malformed list itself, not over a well-formed one
			// that is then swapped in. That is the difference between "the list is
			// refused" and "the signature does not match": a verifier that repairs
			// the list would authenticate this request if the repaired list happened
			// to be the one signed, and refuse it if it were not — which is the same
			// defect expressed as a coin toss. Signing the malformed list makes the
			// only correct answer a refusal.
			signer := newHeaderSigner(req, creds, "us-east-1", unsignedPayloadForTest).
				withSignedList(tc.signedHeaders)
			signer.sign(t)

			assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
		})
	}
}

// unsignedPayloadForTest is the payload marker these negative cases sign, so no
// body has to be produced or hashed.
const unsignedPayloadForTest = "UNSIGNED-PAYLOAD"

// withSignedHeaders replaces the SignedHeaders component of an Authorization
// header, leaving the credential and the signature as they were.
func withSignedHeaders(header, signedHeaders string) string {
	parts := strings.Split(header, ", ")
	for i, part := range parts {
		if strings.HasPrefix(part, "SignedHeaders=") {
			parts[i] = "SignedHeaders=" + signedHeaders
		}
	}
	return strings.Join(parts, ", ")
}

// A header named as signed but absent from the request is refused.
//
// This one was already refused, and it is here because the strictness work touches
// the same code: a name that is in the list and not on the request is a list
// describing a request that is not this one, and it must stay a refusal rather
// than becoming an empty header value that the signature then covers.
func TestASignedHeaderAbsentFromTheRequestIsRefused(t *testing.T) {
	creds := strictCreds
	now := time.Now().UTC().Truncate(time.Second)
	req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayloadForTest)
	signHeaderRequest(t, req, creds, "us-east-1", unsignedPayloadForTest)
	// Name a header that is not on the request, and sign over a list that
	// includes it, so the failure is the missing header rather than the list.
	req.Header.Set("Authorization", withSignedHeaders(req.Header.Get("Authorization"), "host;x-amz-date;x-amz-meta-absent"))

	assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
}

// A signed header sent in any case is found, because the list is lower case and
// the request is not.
//
// HTTP field names are case-insensitive and SigV4 canonicalizes them to lower
// case, so a list naming "x-amz-meta-foo" describes a header the client may have
// sent as "X-Amz-Meta-Foo". Refusing that would be a false negative against a
// conforming signer.
//
// This is the counterpart to the strict cases above, and here because they are
// easy to over-apply: the list's spelling is checked exactly, the request's is
// matched as HTTP says it should be. A request signed over a list naming a header
// it does not carry is still refused, for the signature rather than the spelling.
func TestASignedHeaderIsFoundWhateverCaseTheRequestUsed(t *testing.T) {
	creds := strictCreds
	now := time.Now().UTC().Truncate(time.Second)
	req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayloadForTest)
	// Mixed case on the wire, which is what every SDK sends.
	req.Header.Set("X-Amz-Meta-Foo", "bar")
	newHeaderSigner(req, creds, "us-east-1", unsignedPayloadForTest).
		withSignedList("host;x-amz-content-sha256;x-amz-date;x-amz-meta-foo").
		sign(t)
	if err := auth.Authenticate(req, creds); err != nil {
		t.Fatalf("a request whose signed header used a different case was refused: %v", err)
	}
}

// A well-formed signed request is still accepted.
//
// The negative cases above are only meaningful beside a positive one: a verifier
// that refuses everything would pass all of them.
func TestAWellFormedSignedRequestIsStillAccepted(t *testing.T) {
	creds := strictCreds
	req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", time.Now().UTC().Truncate(time.Second).Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayloadForTest)
	signHeaderRequest(t, req, creds, "us-east-1", unsignedPayloadForTest)
	if err := auth.Authenticate(req, creds); err != nil {
		t.Fatalf("a well-formed signed request was refused: %v", err)
	}
}

// A repeated Authorization component is refused rather than resolved.
//
// The grammar has three components and each appears once. A repeat is a header
// that says two different things, and last-one-wins would make what the request
// means depend on the order the components arrived in — which is the property that
// makes a header carrying a valid credential and a substituted signature
// dangerous.
func TestRepeatedAuthorizationComponentsAreRefused(t *testing.T) {
	creds := strictCreds
	now := time.Now().UTC().Truncate(time.Second)
	build := func() *http.Request {
		req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
		req.Header.Set("Host", "127.0.0.1:9000")
		req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
		req.Header.Set("X-Amz-Content-Sha256", unsignedPayloadForTest)
		signHeaderRequest(t, req, creds, "us-east-1", unsignedPayloadForTest)
		return req
	}

	t.Run("two credentials", func(t *testing.T) {
		req := build()
		// A second, different credential. A last-wins parser reads this as
		// whichever of the two arrived last, so the header's meaning depends on
		// the order two components were written in.
		req.Header.Set("Authorization", withComponent(req.Header.Get("Authorization"), "Credential",
			"AKIAOTHERKEY0000000/20260101/us-east-1/s3/aws4_request"))
		assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
	})
	t.Run("two signed header lists", func(t *testing.T) {
		req := build()
		req.Header.Set("Authorization", withComponent(req.Header.Get("Authorization"), "SignedHeaders", "host"))
		assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
	})
	t.Run("two signatures", func(t *testing.T) {
		req := build()
		// A second signature, and the first one is the real one. A last-wins parser
		// reads this header as the substituted signature and refuses a request
		// that was correctly signed; a first-wins parser reads it as the real one
		// and accepts a header that carries a signature nobody verified.
		req.Header.Set("Authorization", withComponent(req.Header.Get("Authorization"), "Signature", strings.Repeat("ab", 32)))
		assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
	})
	t.Run("an unknown component", func(t *testing.T) {
		req := build()
		req.Header.Set("Authorization", req.Header.Get("Authorization")+", X-Amz-Security-Token=whatever")
		assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
	})
	t.Run("an empty component", func(t *testing.T) {
		req := build()
		req.Header.Set("Authorization", req.Header.Get("Authorization")+", ")
		assertAuthCode(t, auth.Authenticate(req, creds), "AccessDenied")
	})
}

// withComponent appends a second Authorization component with the given key and
// value, leaving every existing component in place.
//
// It appends rather than replaces, because the case is a *repeat*: a header
// carrying the same component twice. A replacement would produce an ordinary
// header, and this must be about the ambiguity rather than about the value.
func withComponent(header, key, value string) string {
	return header + ", " + key + "=" + value
}

// Ordinary repeated query parameters are untouched.
//
// A signed request may legitimately carry the same key twice, and AWS
// canonicalizes it by sorting the values. Tightening how the authentication
// parameters are read must not change how the rest of the query is
// canonicalized, or every client that sends a repeated parameter would start
// failing signature checks for a reason that has nothing to do with
// authentication.
func TestOrdinaryRepeatedQueryParametersStillCanonicalize(t *testing.T) {
	creds := strictCreds
	req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt?list-type=2&prefix=b&prefix=a", nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", time.Now().UTC().Truncate(time.Second).Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", unsignedPayloadForTest)
	signHeaderRequest(t, req, creds, "us-east-1", unsignedPayloadForTest)
	if err := auth.Authenticate(req, creds); err != nil {
		t.Fatalf("a signed request with a repeated ordinary query parameter was refused: %v", err)
	}
}
