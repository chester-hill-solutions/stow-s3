package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	algorithmAWS4HMACSHA256 = "AWS4-HMAC-SHA256"
	serviceS3               = "s3"
	unsignedPayload         = "UNSIGNED-PAYLOAD"
	emptyPayloadHash        = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	amzDateLayout           = "20060102T150405Z"
	dateStampLayout         = "20060102"
)

type signedRequest struct {
	algorithm     string
	credential    credentialScope
	signedHeaders []string
	signature     string
	amzDate       string
	payloadHash   string
	presigned     bool
	expires       int
}

type credentialScope struct {
	accessKeyID string
	dateStamp   string
	region      string
	service     string
}

// The Authorization header is the same material on every request and is parsed
// by hand, which makes the parse a security boundary: everything downstream
// trusts the access key, the signed headers and the signature it returns. A parse
// that accepts what it should refuse is a hole, and so is one that repairs what
// it should refuse — repairing means interpreting something the client did not
// send, and a verifier that guesses at the client's intent is verifying something
// other than what the client signed.
func parseAuthorizationHeader(value string) (signedRequest, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return signedRequest{}, authError("AccessDenied", "missing Authorization header")
	}

	parts := strings.SplitN(value, " ", 2)
	if len(parts) != 2 || parts[0] != algorithmAWS4HMACSHA256 {
		return signedRequest{}, authError("AccessDenied", "malformed Authorization header")
	}

	var sr signedRequest
	sr.algorithm = parts[0]

	// The grammar has exactly three components, and each may appear once.
	//
	// A repeat is not a variant to reconcile; it is a header that says two
	// different things. Last-one-wins, which is what this did, means the meaning
	// of the header depends on the order the components happen to arrive in: a
	// request carrying a valid Credential and a substituted Signature is read as
	// whichever of the two Signatures came last.
	seen := make(map[string]struct{}, len(authorizationComponents))
	for _, segment := range strings.Split(parts[1], ",") {
		key, value, err := splitAuthorizationComponent(segment)
		if err != nil {
			return signedRequest{}, err
		}
		if err := applyAuthorizationComponent(&sr, key, value, seen); err != nil {
			return signedRequest{}, err
		}
	}

	if sr.credential.accessKeyID == "" || len(sr.signedHeaders) == 0 || sr.signature == "" {
		return signedRequest{}, authError("AccessDenied", "malformed Authorization header")
	}
	return sr, nil
}

// splitAuthorizationComponent splits one `Key=Value` segment.
func splitAuthorizationComponent(segment string) (string, string, error) {
	segment = strings.TrimSpace(segment)
	kv := strings.SplitN(segment, "=", 2)
	if segment == "" || len(kv) != 2 {
		return "", "", authError("AccessDenied", "malformed Authorization header")
	}
	return strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]), nil
}

// applyAuthorizationComponent records one component of the header, refusing a
// repeat and a name the grammar does not define.
func applyAuthorizationComponent(sr *signedRequest, key, value string, seen map[string]struct{}) error {
	// An unrecognized component is refused rather than skipped. The grammar is
	// closed, so an extra component is a claim stow cannot check, and a verifier
	// that ignores the claims it cannot check is verifying a subset of the header
	// and reporting it as the whole.
	if !isAuthorizationComponent(key) {
		return authError("AccessDenied", "malformed Authorization header")
	}
	if _, repeated := seen[key]; repeated {
		return authError("AccessDenied", "malformed Authorization header")
	}
	seen[key] = struct{}{}
	switch key {
	case "Credential":
		scope, err := parseCredentialScope(value)
		if err != nil {
			return err
		}
		sr.credential = scope
	case "SignedHeaders":
		signed, err := parseSignedHeaders(value)
		if err != nil {
			return err
		}
		sr.signedHeaders = signed
	case "Signature":
		sr.signature = value
	}
	return nil
}

// authorizationComponents is the closed set the Authorization grammar defines.
var authorizationComponents = [...]string{"Credential", "SignedHeaders", "Signature"}

func isAuthorizationComponent(key string) bool {
	for _, component := range authorizationComponents {
		if key == component {
			return true
		}
	}
	return false
}

func parseCredentialScope(raw string) (credentialScope, error) {
	parts := strings.Split(raw, "/")
	if len(parts) != 5 || parts[4] != "aws4_request" {
		return credentialScope{}, authError("AccessDenied", "malformed credential scope")
	}
	return credentialScope{
		accessKeyID: parts[0],
		dateStamp:   parts[1],
		region:      parts[2],
		service:     parts[3],
	}, nil
}

// parseSignedHeaders reads the SignedHeaders list strictly, or refuses it.
//
// The list is the client's statement of which headers it covered, and the
// signature is computed over a canonical request that names exactly those. Every
// leniency in here is therefore a way to authenticate a request against a set of
// headers the client did not actually commit to — which this used to have four of:
//
//   - empty entries were dropped, so "host;;x-amz-date" became the same list as
//     "host;x-amz-date". A client that emitted a stray separator, or one whose
//     list was assembled from a filter that produced nothing for a header, got a
//     request accepted against a list it never wrote.
//   - entries were trimmed and lowercased, so "host; X-Amz-Date" and
//     "host;x-amz-date" were the same list, and a header named in a different
//     case than the one on the wire was quietly accepted.
//   - duplicates were kept, so "host;host" named the same header twice, and the
//     canonical request carried it twice while the request carried it once.
//   - the list was sorted, so the order the client wrote was discarded. Ordering
//     is not cosmetic here: the canonical request embeds the list verbatim, so
//     sorting locally and not in the signature would make the two disagree — and
//     a list that is silently reordered is a list whose text is no longer the
//     client's.
//
// AWS's own signer emits lower-case, semicolon-separated, ascending and
// duplicate-free, so a conforming client is unaffected by refusing anything else.
func parseSignedHeaders(raw string) ([]string, error) {
	if raw == "" {
		return nil, authError("AccessDenied", "malformed SignedHeaders")
	}
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if part == "" {
			return nil, authError("AccessDenied", "malformed SignedHeaders")
		}
		if !isHTTPToken(part) {
			return nil, authError("AccessDenied", "malformed SignedHeaders")
		}
		if part != strings.ToLower(part) {
			return nil, authError("AccessDenied", "malformed SignedHeaders")
		}
		if _, repeated := seen[part]; repeated {
			return nil, authError("AccessDenied", "malformed SignedHeaders")
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	if !sort.StringsAreSorted(out) {
		return nil, authError("AccessDenied", "malformed SignedHeaders")
	}
	return out, nil
}

// isHTTPToken reports whether name is a valid RFC 9110 field name.
func isHTTPToken(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return len(name) > 0
}

func containsHeader(headers []string, name string) bool {
	for _, header := range headers {
		if header == name {
			return true
		}
	}
	return false
}

// detectSignedRequest works out which signing scheme a request used.
func detectSignedRequest(r *http.Request) (signedRequest, error) {
	query := r.URL.Query()
	if queryValue(query, "X-Amz-Algorithm") != "" || queryValue(query, "X-Amz-Signature") != "" {
		return parsePresignedQuery(query)
	}
	if err := refuseLegacyScheme(r); err != nil {
		return signedRequest{}, err
	}

	authHeader := r.Header.Get("Authorization")
	sr, err := parseAuthorizationHeader(authHeader)
	if err != nil {
		return signedRequest{}, err
	}

	sr.amzDate = headerValue(r.Header, "X-Amz-Date")
	if sr.amzDate == "" {
		sr.amzDate = headerValue(r.Header, "Date")
	}
	if sr.amzDate == "" {
		return signedRequest{}, authError("AccessDenied", "missing x-amz-date")
	}

	sr.payloadHash = headerValue(r.Header, "X-Amz-Content-Sha256")
	if sr.payloadHash == "" {
		sr.payloadHash = emptyPayloadHash
	}
	return sr, nil
}

func headerValue(headers http.Header, name string) string {
	for k, values := range headers {
		if strings.EqualFold(k, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

func verifySignedRequest(r *http.Request, creds Credentials, region string, maxSkew time.Duration, now time.Time) error {
	sr, err := detectSignedRequest(r)
	if err != nil {
		return err
	}
	if err := validateCredentialScope(r, sr, creds, region); err != nil {
		return err
	}
	if err := validateRequestTime(r, sr, maxSkew, now); err != nil {
		return err
	}
	if err := verifyPayloadHash(r, sr); err != nil {
		return err
	}
	expected, err := computeSignature(r, sr, creds.SecretAccessKey)
	if err != nil {
		return err
	}
	// A signature is hex, so the comparison is over the hex. Case-folding it
	// first would accept a signature that differs from the computed one only in
	// case, which is not a signature anyone computed.
	if !hmac.Equal([]byte(sr.signature), []byte(expected)) {
		return authError("SignatureDoesNotMatch", "signature mismatch")
	}
	return nil
}

func validateCredentialScope(r *http.Request, sr signedRequest, creds Credentials, region string) error {
	if sr.credential.accessKeyID != creds.AccessKeyID {
		return authError("AccessDenied", "unknown access key")
	}
	if sr.credential.service != serviceS3 {
		return authError("AccessDenied", "unsupported service in credential scope")
	}
	if sr.credential.region != region {
		return authError("AccessDenied", "region mismatch")
	}
	if !containsHeader(sr.signedHeaders, "host") {
		return authError("AccessDenied", "host must be signed")
	}
	if !sr.presigned {
		dateHeader := "date"
		if headerValue(r.Header, "X-Amz-Date") != "" {
			dateHeader = "x-amz-date"
		}
		if !containsHeader(sr.signedHeaders, dateHeader) {
			return authError("AccessDenied", "date must be signed")
		}
		if headerValue(r.Header, "X-Amz-Content-Sha256") != "" && !containsHeader(sr.signedHeaders, "x-amz-content-sha256") {
			return authError("AccessDenied", "x-amz-content-sha256 must be signed")
		}
	}
	if !strings.HasPrefix(sr.amzDate, sr.credential.dateStamp) {
		return authError("AccessDenied", "credential date mismatch")
	}
	return nil
}

func validateRequestTime(r *http.Request, sr signedRequest, maxSkew time.Duration, now time.Time) error {
	requestTime, err := parseAmzTime(sr.amzDate)
	if err != nil {
		return err
	}
	if sr.presigned {
		return validatePresignedTime(r, sr, requestTime, now, maxSkew)
	}
	if skew := now.Sub(requestTime); skew > maxSkew || skew < -maxSkew {
		return authError("RequestTimeTooSkewed", "request time skew too large")
	}
	return nil
}

func validatePresignedTime(r *http.Request, sr signedRequest, requestTime, now time.Time, maxSkew time.Duration) error {
	if r.Method != http.MethodGet && r.Method != http.MethodPut && r.Method != http.MethodHead {
		return authError("AccessDenied", "unsupported presigned method")
	}
	// The far edge of the window, not only the near one.
	//
	// A presigned URL is valid from its signing time until its expiry, and the
	// signing time is inside the URL — so a client can put any time it likes
	// there, including one far in the future, and produce a URL that is valid for
	// a very long time. The expiry check alone never noticed: with the signing
	// time in the future, "now is after the expiry" is false for years.
	//
	// An ordinary signed request has no such freedom, because its date is a
	// header the client cannot choose freely without invalidating the signature —
	// and stow checks that header's skew in both directions. A presigned URL
	// carried the same freedom without the same check, which is the asymmetry
	// this closes: both now bound how far the request's own clock may be from the
	// server's, by the verifier's configured allowance.
	if requestTime.After(now.Add(maxSkew)) {
		return authError("RequestTimeTooSkewed", "request time skew too large")
	}
	expiry := requestTime.Add(time.Duration(sr.expires) * time.Second)
	if now.After(expiry) {
		return authError("AccessDenied", "request has expired")
	}
	return nil
}

func verifyPayloadHash(r *http.Request, sr signedRequest) error {
	if sr.presigned {
		return nil
	}
	if strings.EqualFold(sr.payloadHash, unsignedPayload) {
		return nil
	}
	if len(sr.payloadHash) != 64 {
		return authError("AccessDenied", "invalid x-amz-content-sha256")
	}
	bodyHash, err := hashRequestBody(r)
	if err != nil {
		return err
	}
	if !strings.EqualFold(bodyHash, sr.payloadHash) {
		return authError("SignatureDoesNotMatch", "payload hash mismatch")
	}
	return nil
}

func hashRequestBody(r *http.Request) (string, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return emptyPayloadHash, nil
	}
	// Body hashing is only required when the client signed the payload hash.
	// Callers that need streaming verification should provide GetBody.
	if r.GetBody == nil {
		return "", authError("AccessDenied", "cannot verify signed payload without request body")
	}
	rc, err := r.GetBody()
	if err != nil {
		return "", authError("AccessDenied", "cannot read request body")
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", authError("AccessDenied", "cannot read request body")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func parseAmzTime(raw string) (time.Time, error) {
	if t, err := time.Parse(amzDateLayout, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC1123, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, authError("AccessDenied", "malformed request date")
}

func computeSignature(r *http.Request, sr signedRequest, secret string) (string, error) {
	canonicalRequest, err := buildCanonicalRequest(r, sr)
	if err != nil {
		return "", err
	}
	stringToSign := buildStringToSign(sr.amzDate, sr.credential, canonicalRequest)
	signingKey := deriveSigningKey(secret, sr.credential.dateStamp, sr.credential.region, sr.credential.service)
	return hex.EncodeToString(hmacSHA256(signingKey, stringToSign)), nil
}

func buildCanonicalRequest(r *http.Request, sr signedRequest) (string, error) {
	// The canonical URI is built from the *decoded* path, encoded once.
	//
	// It used to be built from r.URL.EscapedPath(), which is already encoded, and
	// canonicalURIPath encodes each segment again: a key of "a b" arrives as
	// /a%20b and was signed as /a%2520b. Every key in the conformance corpus was
	// made of characters that survive encoding unchanged, and the two hand-written
	// signers in this package built the canonical URI the same wrong way, so the
	// server and its tests agreed with each other and neither agreed with AWS.
	// A real SDK signing "a b" produced 403 SignatureDoesNotMatch.
	//
	// Encoding the decoded path once is what the SigV4 specification asks for and
	// what every SDK does, and it also gets the awkward cases right: a literal plus
	// in a path is a plus, not an encoded space, so "a+b" signs as /a%2Bb rather
	// than being rewritten to "a b".
	canonicalURI := canonicalURIPath(r.URL.Path)
	canonicalQuery := canonicalQueryString(r.URL.RawQuery)
	canonicalHeaders, signedHeaders, err := canonicalHeaders(r, sr.signedHeaders)
	if err != nil {
		return "", err
	}
	if signedHeaders != strings.Join(sr.signedHeaders, ";") {
		return "", authError("AccessDenied", "signed headers mismatch")
	}
	return strings.Join([]string{
		r.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders,
		signedHeaders,
		sr.payloadHash,
	}, "\n"), nil
}

func buildStringToSign(amzDate string, scope credentialScope, canonicalRequest string) string {
	scopeString := fmt.Sprintf("%s/%s/%s/aws4_request", scope.dateStamp, scope.region, scope.service)
	hash := sha256.Sum256([]byte(canonicalRequest))
	return strings.Join([]string{
		algorithmAWS4HMACSHA256,
		amzDate,
		scopeString,
		hex.EncodeToString(hash[:]),
	}, "\n")
}

func deriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))
	return mac.Sum(nil)
}
