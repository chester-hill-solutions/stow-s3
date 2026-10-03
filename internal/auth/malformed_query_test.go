package auth_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

func TestSignedNoQueryRejectsMalformedSemanticSubstitution(t *testing.T) {
	creds := auth.Credentials{AccessKeyID: "test", SecretAccessKey: "secret"}
	for _, query := range []string{"uploadId=attacker&bad=%ZZ", "list-type=2&prefix=private&bad=%ZZ", "uploads=&bad=%", "bad=x;y"} {
		t.Run(query, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "http://localhost/bucket/key", nil)
			req.Header.Set("X-Amz-Date", time.Now().UTC().Format("20060102T150405Z"))
			req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
			signHeaderRequest(t, req, creds, "us-east-1", "UNSIGNED-PAYLOAD")
			if err := auth.Authenticate(req, creds); err != nil {
				t.Fatal(err)
			}
			req.URL.RawQuery = query
			if err := auth.NewVerifier("us-east-1").AuthenticateAt(req, creds, time.Now()); err == nil {
				t.Fatal("valid no-query signature accepted substituted routing query")
			}
		})
	}
}
