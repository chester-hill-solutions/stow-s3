package s3api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestPresignedPUTCannotAcquireUnsignedCopyOperation(t *testing.T) {
	for _, signed := range []bool{false, true} {
		t.Run(map[bool]string{false: "added-after-signing", true: "signed-copy"}[signed], func(t *testing.T) {
			srv, store := regressionServer(t)
			ctx := context.Background()
			if _, err := store.PutObject(ctx, "bucket", "source", strings.NewReader("source bytes"), storage.PutOptions{}); err != nil {
				t.Fatal(err)
			}
			credentials := auth.Credentials{AccessKeyID: "test-key", SecretAccessKey: "test-secret"}
			srv.auth = SigV4Auth(auth.NewVerifier("us-east-1"), credentials)
			req := httptest.NewRequest(http.MethodPut, "http://example.test/bucket/destination?X-Amz-Expires=300", nil)
			if signed {
				req.Header.Set("X-Amz-Copy-Source", "/bucket/source")
			}
			raw, _, err := v4.NewSigner().PresignHTTP(ctx, aws.Credentials{AccessKeyID: credentials.AccessKeyID, SecretAccessKey: credentials.SecretAccessKey}, req, "UNSIGNED-PAYLOAD", "s3", "us-east-1", time.Now().UTC(), func(o *v4.SignerOptions) { o.DisableHeaderHoisting = true })
			if err != nil {
				t.Fatal(err)
			}
			req.URL, err = url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Amz-Copy-Source", "/bucket/source")
			res := httptest.NewRecorder()
			srv.ServeHTTP(res, req)
			if signed {
				if res.Code != http.StatusOK {
					t.Fatalf("signed copy=%d %s", res.Code, res.Body.String())
				}
			} else {
				if res.Code != http.StatusForbidden {
					t.Fatalf("unsigned copy=%d %s", res.Code, res.Body.String())
				}
				if _, err := store.HeadObject(ctx, "bucket", "destination"); !errors.Is(err, storage.ErrObjectNotFound) {
					t.Fatalf("unsigned copy mutated destination: %v", err)
				}
			}
		})
	}
}
