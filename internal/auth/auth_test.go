package auth_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

func TestGenerateCredentials(t *testing.T) {
	creds, err := auth.GenerateCredentials()
	if err != nil {
		t.Fatalf("GenerateCredentials: %v", err)
	}
	if len(creds.AccessKeyID) != 20 {
		t.Fatalf("access key length = %d", len(creds.AccessKeyID))
	}
	if len(creds.SecretAccessKey) != 40 {
		t.Fatalf("secret key length = %d", len(creds.SecretAccessKey))
	}

	other, err := auth.GenerateCredentials()
	if err != nil {
		t.Fatalf("GenerateCredentials: %v", err)
	}
	if creds == other {
		t.Fatal("expected unique credentials")
	}
}

func TestAuthenticateHeaderAuthRoundTrip(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}

	req := httptestRequest(t, http.MethodGet, "http://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Host", "examplebucket.s3.amazonaws.com")
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("X-Amz-Content-Sha256", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	req.Header.Set("X-Amz-Date", "20130524T000000Z")
	signHeaderRequest(t, req, creds, "us-east-1", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	verifier := auth.NewVerifier("us-east-1")
	if err := verifier.AuthenticateAt(req, creds, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("AuthenticateAt: %v", err)
	}
}

func TestAuthenticateUnsignedPayloadPUT(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte("payload")
	req := httptestRequest(t, http.MethodPut, "http://127.0.0.1:9000/demo/object.txt", bytes.NewReader(body))
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	req.Header.Set("Content-Length", "7")

	signHeaderRequest(t, req, creds, "us-east-1", "UNSIGNED-PAYLOAD")

	verifier := auth.NewVerifier("us-east-1")
	if err := verifier.Authenticate(req, creds); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticateSignedPayloadRequiresBodyHash(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	body := []byte("hello")
	hash := sha256.Sum256(body)
	now := time.Now().UTC().Truncate(time.Second)

	req := httptestRequest(t, http.MethodPut, "http://127.0.0.1:9000/demo/object.txt", bytes.NewReader(body))
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(hash[:]))
	signHeaderRequest(t, req, creds, "us-east-1", hex.EncodeToString(hash[:]))

	verifier := auth.NewVerifier("us-east-1")
	if err := verifier.Authenticate(req, creds); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticatePresignedGET(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	now := time.Now().UTC().Truncate(time.Second)
	reqURL := signPresignedURL(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", creds, "us-east-1", now, 300, nil)

	req := httptestRequest(t, http.MethodGet, reqURL, nil)
	req.Header.Set("Host", "127.0.0.1:9000")

	if err := auth.Authenticate(req, creds); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticatePresignedPUT(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	now := time.Now().UTC().Truncate(time.Second)
	reqURL := signPresignedURL(t, http.MethodPut, "http://127.0.0.1:9000/demo/upload.bin", creds, "us-east-1", now, 600, map[string]string{
		"Content-Type": "text/plain",
	})

	req := httptestRequest(t, http.MethodPut, reqURL, strings.NewReader("uploaded"))
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("Content-Type", "text/plain")

	if err := auth.Authenticate(req, creds); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
}

func TestAuthenticateWrongSecret(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	now := time.Now().UTC().Truncate(time.Second)
	req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	signHeaderRequest(t, req, creds, "us-east-1", "UNSIGNED-PAYLOAD")

	wrong := creds
	wrong.SecretAccessKey = "WRONGSECRETWRONGSECRETWRONGSECRET12"
	err := auth.Authenticate(req, wrong)
	assertAuthCode(t, err, "SignatureDoesNotMatch")
}

func TestAuthenticateExpiredPresignedURL(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	past := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	reqURL := signPresignedURL(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", creds, "us-east-1", past, 60, nil)

	req := httptestRequest(t, http.MethodGet, reqURL, nil)
	req.Header.Set("Host", "127.0.0.1:9000")

	err := auth.Authenticate(req, creds)
	assertAuthCode(t, err, "AccessDenied")
}

func TestAuthenticateRejectsOversizedPresignedExpiry(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	now := time.Now().UTC().Truncate(time.Second)
	reqURL := signPresignedURL(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", creds, "us-east-1", now, 604801, nil)
	req := httptestRequest(t, http.MethodGet, reqURL, nil)
	req.Header.Set("Host", "127.0.0.1:9000")

	err := auth.Authenticate(req, creds)
	assertAuthCode(t, err, "AccessDenied")
}

func TestAuthenticateRequestTimeTooSkewed(t *testing.T) {
	creds := auth.Credentials{
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "SECRETKEYEXAMPLESECRETKEYEXAMPLE12",
	}
	stale := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	req := httptestRequest(t, http.MethodGet, "http://127.0.0.1:9000/demo/object.txt", nil)
	req.Header.Set("Host", "127.0.0.1:9000")
	req.Header.Set("X-Amz-Date", stale.Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	signHeaderRequest(t, req, creds, "us-east-1", "UNSIGNED-PAYLOAD")

	err := auth.Authenticate(req, creds)
	assertAuthCode(t, err, "RequestTimeTooSkewed")
}

func assertAuthCode(t *testing.T, err error, code string) {
	t.Helper()
	var authErr *auth.Error
	if !errors.As(err, &authErr) {
		t.Fatalf("expected auth.Error, got %T (%v)", err, err)
	}
	if authErr.Code != code {
		t.Fatalf("code = %q, want %q", authErr.Code, code)
	}
}

func httptestRequest(t *testing.T, method, rawURL string, body io.Reader) *http.Request {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
	}
	req, err := http.NewRequest(method, rawURL, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}
	return req
}

// signHeaderRequest signs a request over the standard header list. A case that
// needs a different list uses newHeaderSigner(req, creds, region, hash).withSignedList.
func signHeaderRequest(t *testing.T, req *http.Request, creds auth.Credentials, region, payloadHash string) {
	t.Helper()
	signer := newHeaderSigner(req, creds, region, payloadHash).withSignedList("host;x-amz-content-sha256;x-amz-date")
	// Sorted, because that list is already the conforming form and every other
	// caller of this helper means a well-formed one.
	entries := strings.Split(signer.signedList, ";")
	sort.Strings(entries)
	signer.signedList = strings.Join(entries, ";")
	signer.sign(t)
}

// headerSigner is a request, a credential set and a signing context, with the
// SignedHeaders list as the one thing that varies between cases. It exists so a
// case that needs a different list says so by name rather than by passing six
// arguments to a helper.
type headerSigner struct {
	req         *http.Request
	creds       auth.Credentials
	region      string
	payloadHash string
	signedList  string
}

func newHeaderSigner(req *http.Request, creds auth.Credentials, region, payloadHash string) *headerSigner {
	return &headerSigner{req: req, creds: creds, region: region, payloadHash: payloadHash}
}

// withSignedList names the headers the signature will cover. The list must be the
// lower-case, semicolon-separated, ascending form AWS's signer emits.
func (s *headerSigner) withSignedList(list string) *headerSigner {
	s.signedList = list
	return s
}

func (s *headerSigner) sign(t *testing.T) {
	t.Helper()
	amzDate := s.req.Header.Get("X-Amz-Date")
	dateStamp := amzDate[:8]

	// The list is signed as written, not sorted: the canonical request embeds it
	// verbatim, and a case that signs a repaired list could not tell a verifier
	// that repairs one from one that refuses it.
	signedHeaders := strings.Split(s.signedList, ";")
	canonicalHeaders := buildCanonicalHeaders(s.req, signedHeaders)
	canonicalRequest := strings.Join([]string{
		s.req.Method,
		// The decoded path, encoded once - see buildCanonicalRequest. These two
		// signers used the escaped path, which double-encoded any key needing it
		// and so agreed with the server while both were wrong. conformance/
		// encoding_test.go is the case that would have caught it, because it signs
		// with the real SDK; nothing here exercises a key that needs encoding.
		canonicalURI(s.req.URL.Path),
		canonicalQuery(s.req.URL.RawQuery),
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		s.payloadHash,
	}, "\n")

	signature := signString(s.creds.SecretAccessKey, dateStamp, s.region, "s3", amzDate, canonicalRequest)
	s.req.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s/%s/s3/aws4_request, SignedHeaders=%s, Signature=%s",
		"AWS4-HMAC-SHA256",
		s.creds.AccessKeyID,
		dateStamp,
		s.region,
		strings.Join(signedHeaders, ";"),
		signature,
	))
}

func signPresignedURL(t *testing.T, method, rawURL string, creds auth.Credentials, region string, signingTime time.Time, expires int, extraHeaders map[string]string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}

	amzDate := signingTime.UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]
	query := parsed.Query()
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", fmt.Sprintf("%s/%s/%s/s3/aws4_request", creds.AccessKeyID, dateStamp, region))
	query.Set("X-Amz-Date", amzDate)
	query.Set("X-Amz-Expires", fmt.Sprintf("%d", expires))

	signedHeaders := []string{"host"}
	for name := range extraHeaders {
		signedHeaders = append(signedHeaders, strings.ToLower(name))
	}
	sort.Strings(signedHeaders)
	query.Set("X-Amz-SignedHeaders", strings.Join(signedHeaders, ";"))
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequest(method, parsed.String(), nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Host", parsed.Host)
	for name, value := range extraHeaders {
		req.Header.Set(name, value)
	}

	canonicalHeaders := buildCanonicalHeaders(req, signedHeaders)
	canonicalQuery := canonicalQuery(req.URL.RawQuery)
	canonicalRequest := strings.Join([]string{
		method,
		// See the note at the other signer: the decoded path, encoded once.
		canonicalURI(parsed.Path),
		canonicalQuery,
		canonicalHeaders,
		strings.Join(signedHeaders, ";"),
		"UNSIGNED-PAYLOAD",
	}, "\n")

	signature := signString(creds.SecretAccessKey, dateStamp, region, "s3", amzDate, canonicalRequest)
	finalQuery, err := url.ParseQuery(req.URL.RawQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	finalQuery.Set("X-Amz-Signature", signature)
	parsed.RawQuery = finalQuery.Encode()
	return parsed.String()
}

func signString(secret, dateStamp, region, service, amzDate, canonicalRequest string) string {
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	hash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(hash[:]),
	}, "\n")
	key := deriveSigningKey(secret, dateStamp, region, service)
	return hex.EncodeToString(hmacSHA256(key, stringToSign))
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

func buildCanonicalHeaders(req *http.Request, signed []string) string {
	ordered := append([]string(nil), signed...)
	sort.Strings(ordered)
	var b strings.Builder
	for _, name := range ordered {
		var value string
		switch name {
		case "host":
			value = req.Header.Get("Host")
			if value == "" {
				value = req.Host
			}
		default:
			value = req.Header.Get(name)
		}
		b.WriteString(name)
		b.WriteByte(':')
		b.WriteString(strings.Join(strings.Fields(strings.TrimSpace(value)), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		segments[i] = uriEncode(segment, false)
	}
	return strings.Join(segments, "/")
}

func canonicalQuery(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return ""
	}
	for key := range values {
		if strings.EqualFold(key, "X-Amz-Signature") {
			delete(values, key)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		vals := values[key]
		sort.Strings(vals)
		encodedKey := uriEncode(key, true)
		for _, val := range vals {
			parts = append(parts, encodedKey+"="+uriEncode(val, true))
		}
	}
	return strings.Join(parts, "&")
}

func uriEncode(value string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		if c == '/' && !encodeSlash {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}
