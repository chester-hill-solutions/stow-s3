package auth

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The refusal is right and stays — sigv4_strictness_test.go guards that SigV2 is not
// implemented. This file is only about whether the refusal says something true.

func presignedSigV2(accessKey, signature string) url.Values {
	return url.Values{
		"AWSAccessKeyId": {accessKey},
		"Signature":      {signature},
		"Expires":        {"1893456000"},
	}
}

func TestASigV2PresignedRequestIsRefusedByName(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet,
		"http://127.0.0.1/mybucket/thing.txt?"+presignedSigV2("AKIAEXAMPLE", "c2lnbmF0dXJl").Encode(), nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	_, err = detectSignedRequest(request)
	if err == nil {
		t.Fatal("a SigV2 presigned request was accepted, and this server does not implement SigV2")
	}
	message := err.Error()
	if !strings.Contains(message, "Version 2") {
		t.Errorf("the refusal %q does not say the request was signed with SigV2", message)
	}
	if !strings.Contains(message, "Version 4") {
		t.Errorf("the refusal %q does not say what to sign with instead", message)
	}
	if strings.Contains(message, "missing Authorization") {
		t.Errorf("the refusal %q tells a SigV2 presigned request it has no Authorization header, which is false and impossible to fix for a presigned URL", message)
	}
}

func TestASigV2AuthorizationHeaderIsRefusedByName(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/mybucket/thing.txt", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	request.Header.Set("Authorization", "AWS AKIAEXAMPLE:c2lnbmF0dXJl")

	if _, err := detectSignedRequest(request); err == nil {
		t.Fatal("a SigV2 Authorization header was accepted")
	} else if !strings.Contains(err.Error(), "Version 2") {
		t.Errorf("the refusal %q does not name SigV2, and the generic 'malformed Authorization header' sends the caller looking for a syntax error that is not there", err)
	}
}

func TestAPartialSigV2ParameterPairIsNotTreatedAsSigV2(t *testing.T) {
	for name, query := range map[string]url.Values{
		"access key only": {"AWSAccessKeyId": {"AKIAEXAMPLE"}},
		"signature only":  {"Signature": {"c2lnbmF0dXJl"}},
	} {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodGet,
				"http://127.0.0.1/mybucket/thing.txt?"+query.Encode(), nil)
			if err != nil {
				t.Fatalf("build the request: %v", err)
			}
			_, err = detectSignedRequest(request)
			if err != nil && strings.Contains(err.Error(), "Version 2") {
				t.Errorf("a request carrying only %q was refused as SigV2, so a caller with an unrelated query parameter is told to change their signing algorithm", name)
			}
		})
	}
}

func TestTheSigV2RefusalDidNotDisturbSigV4OrUnsignedRequests(t *testing.T) {
	sigv4 := url.Values{
		"X-Amz-Algorithm":     {algorithmAWS4HMACSHA256},
		"X-Amz-Credential":    {"AKIAEXAMPLE/20260101/us-east-1/s3/aws4_request"},
		"X-Amz-Date":          {"20260101T000000Z"},
		"X-Amz-Expires":       {"600"},
		"X-Amz-SignedHeaders": {"host"},
		"X-Amz-Signature":     {"deadbeef"},
	}
	signed, err := http.NewRequest(http.MethodGet,
		"http://127.0.0.1/mybucket/thing.txt?"+sigv4.Encode(), nil)
	if err != nil {
		t.Fatalf("build the SigV4 request: %v", err)
	}
	if _, err := detectSignedRequest(signed); err != nil {
		t.Errorf("a SigV4 presigned request was refused with %v, so the SigV2 check is catching it", err)
	}

	unsigned, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/mybucket/thing.txt", nil)
	if err != nil {
		t.Fatalf("build the unsigned request: %v", err)
	}
	_, err = detectSignedRequest(unsigned)
	if err == nil {
		t.Error("an unsigned request was accepted")
	} else if !strings.Contains(err.Error(), "missing Authorization") {
		t.Errorf("an unsigned request is refused with %q, and the SigV2 check is catching a request that is not signed at all", err)
	}
}
