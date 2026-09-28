package auth

import (
	"net/http"
	"net/url"
	"strings"
)

// Only SigV4 is implemented. SigV2 is refused by name, in either of its two forms.
const (
	legacySigV2Scheme = "AWS "

	sigV2Refusal = "this request is signed with Signature Version 2, which this server does not implement. " +
		"Sign the request with AWS Signature Version 4"
)

// isSigV2Presigned requires both parameters. AWSAccessKeyId alone and Signature
// alone are both ordinary query parameters, and treating either as a signing scheme
// would tell a caller to change their algorithm over an unrelated parameter.
func isSigV2Presigned(query url.Values) bool {
	return queryValue(query, "AWSAccessKeyId") != "" && queryValue(query, "Signature") != ""
}

// refuseLegacyScheme reports SigV2 as SigV2. The presigned form cannot be given an
// Authorization header at all, so a refusal naming a missing header asks the caller
// to do something impossible.
func refuseLegacyScheme(r *http.Request) error {
	switch {
	case isSigV2Presigned(r.URL.Query()):
		return authError("AccessDenied",
			"%s: set X-Amz-Algorithm to %s, or configure your client with signature_version=s3v4",
			sigV2Refusal, algorithmAWS4HMACSHA256)
	case strings.HasPrefix(strings.TrimSpace(r.Header.Get("Authorization")), legacySigV2Scheme):
		return authError("AccessDenied",
			"%s, whose Authorization header begins %s", sigV2Refusal, algorithmAWS4HMACSHA256)
	}
	return nil
}
