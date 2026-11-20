package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamingPayloadHashIsRefusedByName(t *testing.T) {
	request := httptest.NewRequest("PUT", "/bucket/key", strings.NewReader("body"))
	err := verifyPayloadHash(request, signedRequest{payloadHash: "STREAMING-AWS4-HMAC-SHA256-PAYLOAD"})
	authErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("streaming payload error = %v", err)
	}
	if authErr.Code != "NotImplemented" || !strings.Contains(authErr.Message, "STREAMING-AWS4-HMAC-SHA256-PAYLOAD") {
		t.Fatalf("streaming payload refusal = %+v", authErr)
	}
}
