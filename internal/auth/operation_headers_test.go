package auth_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

func TestPresignedOperationHeadersMustBeSigned(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, header := range []string{"X-Amz-Copy-Source", "X-Amz-Copy-Source-If-Match", "X-Amz-Meta-Owner", "X-Amz-Checksum-Sha256"} {
		t.Run(header, func(t *testing.T) {
			for _, signed := range []bool{false, true} {
				headers := map[string]string{}
				if signed {
					headers[header] = "source"
				}
				url := signPresignedURL(t, http.MethodPut, "http://127.0.0.1:9000/demo/key", strictCreds, "us-east-1", now, 300, headers)
				req := httptestRequest(t, http.MethodPut, url, strings.NewReader(""))
				req.Header.Set(header, "source")
				err := auth.NewVerifier("us-east-1").AuthenticateAt(req, strictCreds, now)
				if signed && err != nil || !signed && err == nil {
					t.Fatalf("signed=%v: %v", signed, err)
				}
			}
		})
	}
}

func TestHeaderAuthenticationRejectsUnsignedCopySource(t *testing.T) {
	req := httptestRequest(t, http.MethodPut, "http://127.0.0.1:9000/demo/key", nil)
	req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
	req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
	signHeaderRequest(t, req, strictCreds, "us-east-1", "UNSIGNED-PAYLOAD")
	req.Header.Set("X-Amz-Copy-Source", "/demo/secret")
	if err := auth.Authenticate(req, strictCreds); err == nil {
		t.Fatal("unsigned copy source authenticated")
	}
}
