package s3api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestAuthenticatedHTTPPutAllocationBounded(t *testing.T) {
	result := testing.Benchmark(BenchmarkAuthenticatedHTTPPut)
	if result.AllocedBytesPerOp() > 1536<<10 {
		t.Fatalf("authenticated HTTP PUT duplicates its owned body: %d allocated bytes/op for a 1 MiB object", result.AllocedBytesPerOp())
	}
}

func BenchmarkAuthenticatedHTTPPut(b *testing.B) {
	benchmarkAuthenticatedHTTPPut(b, false)
}

func BenchmarkAuthenticatedHTTPPutParallel(b *testing.B) {
	benchmarkAuthenticatedHTTPPut(b, true)
}

func benchmarkAuthenticatedHTTPPut(b *testing.B, parallel bool) {
	ctx := context.Background()
	instance, err := runtime.Open(runtime.Options{MaxBytes: 8 << 20, MaxObjects: 4})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { instance.Close() })
	if err := instance.CreateBucket(ctx, "assets"); err != nil {
		b.Fatal(err)
	}
	adapter, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		b.Fatal(err)
	}
	credentials := auth.Credentials{AccessKeyID: "allocation-test", SecretAccessKey: "allocation-test-secret"}
	server, err := New(Config{Store: adapter, Auth: SigV4Auth(auth.NewVerifier("us-east-1"), credentials)})
	if err != nil {
		b.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), 1<<20)
	request := signedAllocationRequest(b, payload, credentials)
	oldLog := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(oldLog) })
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	runAuthenticatedPutRequests(b, server, request, payload, parallel)
	b.StopTimer()
	payload[0] = 'z'
	object, err := instance.GetObject(ctx, "assets", "target")
	if err != nil {
		b.Fatal(err)
	}
	if len(object.Data) != 1<<20 || object.Data[0] != 'x' {
		b.Fatal("HTTP upload retained the caller's request buffer")
	}
}

func runAuthenticatedPutRequests(b *testing.B, server *Server, request *http.Request, payload []byte, parallel bool) {
	if parallel {
		b.RunParallel(func(worker *testing.PB) {
			for worker.Next() {
				sendAuthenticatedPut(b, server, request, payload)
			}
		})
		return
	}
	for index := 0; index < b.N; index++ {
		sendAuthenticatedPut(b, server, request, payload)
	}
}

func sendAuthenticatedPut(b *testing.B, server *Server, request *http.Request, payload []byte) {
	current := request.Clone(context.Background())
	current.Body = io.NopCloser(bytes.NewReader(payload))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, current)
	if response.Code != http.StatusOK {
		b.Fatalf("signed PUT: %d %s", response.Code, response.Body.String())
	}
}

func signedAllocationRequest(b *testing.B, payload []byte, credentials auth.Credentials) *http.Request {
	b.Helper()
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1/assets/target", bytes.NewReader(payload))
	request.Header.Set("Content-Length", strconv.Itoa(len(payload)))
	hash := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(hash[:])
	request.Header.Set("x-amz-content-sha256", payloadHash)
	checksum, err := storage.ComputeChecksum("CRC32", payload)
	if err != nil {
		b.Fatal(err)
	}
	request.Header.Set("x-amz-checksum-crc32", checksum)
	if err := v4.NewSigner().SignHTTP(context.Background(), aws.Credentials{AccessKeyID: credentials.AccessKeyID, SecretAccessKey: credentials.SecretAccessKey}, request, payloadHash, "s3", "us-east-1", time.Now().UTC()); err != nil {
		b.Fatal(err)
	}
	return request
}
