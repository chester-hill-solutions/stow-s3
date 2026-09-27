package auth

// Presigned-query parsing: the authentication parameters, read one name at a time
// and each exactly once.
//
// Separate from the header-auth parsing because the failure it prevents is
// different in kind. A malformed Authorization header is the client saying
// something stow cannot read; an ambiguous presigned query is two readings of one
// string disagreeing, and the values stow consumes come from the same string the
// signature was computed over.

import (
	"net/url"
	"strconv"
	"strings"
)

// maxPresignedExpirySeconds is S3's ceiling on X-Amz-Expires: seven days.
const maxPresignedExpirySeconds = 604800

// The presigned authentication parameters, read one name at a time and each
// exactly once.
//
// This used to be a case-insensitive scan that returned the first match, so
// X-Amz-Date and x-amz-date were the same parameter and whichever the map
// happened to yield first was the one used. That is a real ambiguity rather than
// a cosmetic one, because the signed query and the values consumed here are
// parsed from the same string: a query that carried a differently-cased duplicate
// could be signed as one parameter and read as another, and a request could carry
// two signatures and be checked against whichever one the scan reached first.
//
// So the rule here is one deterministic interpretation: the parameter names are
// exact, and a name that appears more than once — or that appears under a
// different case than the one stow reads — is refused. Failing closed is the only
// safe answer, because the alternative is choosing between two claims and having
// no way to tell which one the client meant.
//
// The canonical query string is built separately, and its duplicate handling is
// unchanged: ordinary repeated query parameters are a normal part of a request and
// AWS canonicalizes them by sorting, so that behaviour is not touched here.
func parsePresignedQuery(query url.Values) (signedRequest, error) {
	algorithm, err := requiredAuthQueryValue(query, "X-Amz-Algorithm", "missing presigned auth parameters")
	if err != nil {
		return signedRequest{}, err
	}
	if algorithm != algorithmAWS4HMACSHA256 {
		return signedRequest{}, authError("AccessDenied", "unsupported presigned algorithm")
	}

	scope, err := presignedCredentialScope(query)
	if err != nil {
		return signedRequest{}, err
	}
	amzDate, err := requiredAuthQueryValue(query, "X-Amz-Date", "missing X-Amz-Date")
	if err != nil {
		return signedRequest{}, err
	}
	expires, err := presignedExpiry(query)
	if err != nil {
		return signedRequest{}, err
	}
	signedHeaders, err := presignedSignedHeaders(query)
	if err != nil {
		return signedRequest{}, err
	}
	signature, err := requiredAuthQueryValue(query, "X-Amz-Signature", "missing X-Amz-Signature")
	if err != nil {
		return signedRequest{}, err
	}

	return signedRequest{
		algorithm:     algorithm,
		credential:    scope,
		signedHeaders: signedHeaders,
		signature:     signature,
		amzDate:       amzDate,
		payloadHash:   unsignedPayload,
		presigned:     true,
		expires:       expires,
	}, nil
}

// requiredAuthQueryValue reads one authentication parameter and refuses an absent
// or empty one, with the caller's message for which one it was.
func requiredAuthQueryValue(query url.Values, key, missingMessage string) (string, error) {
	value, err := authQueryValue(query, key)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", authError("AccessDenied", "%s", missingMessage)
	}
	return value, nil
}

func presignedCredentialScope(query url.Values) (credentialScope, error) {
	raw, err := authQueryValue(query, "X-Amz-Credential")
	if err != nil {
		return credentialScope{}, err
	}
	return parseCredentialScope(raw)
}

// presignedExpiry reads X-Amz-Expires and refuses anything outside S3's range: a
// non-integer, zero or negative, and anything past the seven-day ceiling.
//
// The ceiling is the only thing standing between a presigned URL and a permanent
// one, so an unusable value is refused rather than clamped or defaulted: a
// clamped 604801 would silently become a seven-day URL, and a defaulted one
// would silently become whatever the default is.
func presignedExpiry(query url.Values) (int, error) {
	raw, err := authQueryValue(query, "X-Amz-Expires")
	if err != nil {
		return 0, err
	}
	expires, convErr := strconv.Atoi(raw)
	if convErr != nil || expires <= 0 || expires > maxPresignedExpirySeconds {
		return 0, authError("AccessDenied", "invalid X-Amz-Expires")
	}
	return expires, nil
}

func presignedSignedHeaders(query url.Values) ([]string, error) {
	raw, err := authQueryValue(query, "X-Amz-SignedHeaders")
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, authError("AccessDenied", "missing X-Amz-SignedHeaders")
	}
	return parseSignedHeaders(raw)
}

// authQueryValue reads one SigV4 authentication parameter, and refuses a query
// that does not name it exactly once.
//
// Exact match, because the signed query and the values read here are two readings
// of the same string: an interpretation that treats X-Amz-Date and x-amz-date as
// one parameter can be handed a query where the two spellings disagree and answer
// with a different value than the one that was signed.
//
// Refused rather than resolved when it appears more than once, or under another
// spelling, because there is no correct choice to make. Picking one would make
// what stow authenticates depend on iteration order.
func authQueryValue(query url.Values, key string) (string, error) {
	values, exact := query[key]
	if !exact {
		if queryHasFoldedVariant(query, key) {
			return "", authError("AccessDenied", "ambiguous presigned authentication parameter %s", key)
		}
		return "", nil
	}
	if len(values) != 1 {
		return "", authError("AccessDenied", "repeated presigned authentication parameter %s", key)
	}
	if queryHasFoldedVariant(query, key) {
		return "", authError("AccessDenied", "ambiguous presigned authentication parameter %s", key)
	}
	return values[0], nil
}

// queryHasFoldedVariant reports whether the query carries key under any other
// spelling. The map already holds each spelling separately, so this is a question
// about the key set and nothing more.
func queryHasFoldedVariant(query url.Values, key string) bool {
	for name := range query {
		if name != key && strings.EqualFold(name, key) {
			return true
		}
	}
	return false
}

// queryValue reads a query parameter case-insensitively, first match wins.
//
// It is for the detection of *whether* a request is presigned, not for reading an
// authentication value: detectSignedRequest needs to know that a signature is
// present under any spelling, and every value it goes on to consume goes through
// authQueryValue, which refuses anything ambiguous.
func queryValue(query url.Values, key string) string {
	for k, values := range query {
		if strings.EqualFold(k, key) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
