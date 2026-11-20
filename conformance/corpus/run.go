package corpus

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/chester-hill-solutions/stow-s3/internal/adapterbridge"
	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow/persistence"
)

const testRegion = auth.DefaultRegion

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

type StatusReporter interface {
	StatusCode() int
}

type Env struct {
	Client       *s3.Client
	Endpoint     string
	Credentials  Credentials
	Status       StatusReporter
	Capabilities map[string]bool
}

func Run(t *testing.T, newEnv func(t *testing.T) *Env) {
	t.Helper()
	cases := loadSharedCorpus(t)
	for _, testCase := range cases.Cases {
		testCase := testCase
		t.Run(testCase.ID, func(t *testing.T) {
			env := newEnv(t)
			if env == nil {
				t.Fatal("corpus environment is required")
			}
			env.prepare(t)
			for _, requirement := range testCase.Requires {
				if env.Capabilities != nil && !env.Capabilities[requirement] {
					t.Skipf("this backend does not provide %s", requirement)
				}
			}
			runSharedCorpusCase(t, env, testCase)
		})
	}
}

func (e *Env) prepare(t *testing.T) {
	t.Helper()
	if e.Client != nil {
		if e.Status == nil {
			t.Fatal("corpus.Env.Status is required when Client is supplied")
		}
		return
	}
	if e.Endpoint == "" {
		t.Fatal("corpus.Env.Endpoint or Client is required")
	}
	capture := &statusCapture{}
	e.Status = capture
	e.Client = newClient(e.Endpoint, e.Credentials, capture)
}

func RunAdapter(t *testing.T, newAdapter func(t *testing.T) persistence.Adapter) {
	t.Helper()
	Run(t, func(t *testing.T) *Env {
		backend := newAdapter(t)
		store, err := adapterbridge.Bind(backend)
		if err != nil {
			t.Fatalf("adapter bridge: %v", err)
		}
		return serve(t, store, map[string]bool{"multipart": false, "checksum": true, "copy": true, "conditional": true})
	})
}

func serve(t *testing.T, store storage.Store, capabilities map[string]bool) *Env {
	t.Helper()
	creds := Credentials{AccessKeyID: "AKIACONFORMANCETEST01", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}
	verifier := auth.NewVerifier(testRegion)
	server, err := s3api.New(s3api.Config{
		Store: store, Auth: s3api.SigV4Auth(verifier, auth.Credentials{
			AccessKeyID: creds.AccessKeyID, SecretAccessKey: creds.SecretAccessKey,
		}), Host: "127.0.0.1", Port: 0, Region: testRegion,
	})
	if err != nil {
		t.Fatalf("s3api.New: %v", err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.ListenAndServe() }()
	deadline := time.Now().Add(2 * time.Second)
	for server.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if server.Addr() == "" {
		t.Fatal("server failed to bind")
	}
	endpoint := "http://" + server.Addr()
	status := &statusCapture{}
	env := &Env{Endpoint: endpoint, Credentials: creds, Status: status, Capabilities: capabilities}
	env.Client = newClient(endpoint, creds, status)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		select {
		case err := <-errCh:
			if err != nil && err != http.ErrServerClosed {
				t.Logf("server exit: %v", err)
			}
		case <-time.After(2 * time.Second):
		}
		if err := store.Close(); err != nil {
			t.Logf("store close: %v", err)
		}
	})
	return env
}

func newClient(endpoint string, creds Credentials, status *statusCapture) *s3.Client {
	cfg := aws.Config{
		Region:      testRegion,
		Credentials: credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken),
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
		options.APIOptions = append(options.APIOptions, status.capture)
	})
}

type statusCapture struct {
	mu     sync.Mutex
	status int
}

func (c *statusCapture) capture(stack *middleware.Stack) error {
	return stack.Deserialize.Add(middleware.DeserializeMiddlewareFunc("corpus-status", func(ctx context.Context, in middleware.DeserializeInput, next middleware.DeserializeHandler) (middleware.DeserializeOutput, middleware.Metadata, error) {
		out, metadata, err := next.HandleDeserialize(ctx, in)
		if err == nil {
			if response, ok := out.RawResponse.(*smithyhttp.Response); ok && response.Response != nil {
				c.mu.Lock()
				c.status = response.StatusCode
				c.mu.Unlock()
			}
		}
		return out, metadata, err
	}), middleware.After)
}

func (c *statusCapture) StatusCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func readCorpus() ([]byte, error) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("corpus: source location unavailable")
	}
	return os.ReadFile(filepath.Join(filepath.Dir(source), "cases.json"))
}

func UniqueBucket(t *testing.T, parts ...string) string {
	t.Helper()
	name := t.Name()
	for _, part := range parts {
		name += "/" + part
	}
	sum := sha256.Sum256([]byte(name))
	return fmt.Sprintf("conf-%x", sum[:8])
}

func CreateBucket(ctx context.Context, t *testing.T, client *s3.Client, bucket string) {
	t.Helper()
	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
		t.Fatalf("CreateBucket(%q): %v", bucket, err)
	}
}
