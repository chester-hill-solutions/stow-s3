package s3api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestConcurrentRequestAdmissionBeforeBodyRead(t *testing.T) {
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	server, err := New(Config{Store: storage.NewMemoryStore(), MaxConcurrentRequests: 1, Auth: func(*http.Request) error {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		return errors.New("test auth refusal")
	}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	<-entered
	body := &admissionBody{Reader: bytes.NewReader([]byte("payload"))}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/bucket/key", body))
	close(release)
	<-done
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "SlowDown") {
		t.Fatalf("saturation response=%d %s", response.Code, response.Body.String())
	}
	if body.reads != 0 || calls.Load() != 1 {
		t.Fatalf("rejected request was processed: reads=%d auth=%d", body.reads, calls.Load())
	}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code == http.StatusServiceUnavailable || calls.Load() != 2 {
		t.Fatal("auth error leaked permit")
	}
}

type admissionBody struct {
	io.Reader
	reads int
}

func (b *admissionBody) Read(data []byte) (int, error) { b.reads++; return b.Reader.Read(data) }

func TestConcurrentRequestPermitReleasedAfterCancellation(t *testing.T) {
	entered := make(chan struct{})
	server, err := New(Config{Store: storage.NewMemoryStore(), MaxConcurrentRequests: 1, Auth: func(request *http.Request) error {
		close(entered)
		<-request.Context().Done()
		return request.Context().Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	}()
	<-entered
	cancel()
	<-done
	if len(server.requestSlots) != 0 {
		t.Fatal("cancellation leaked request permit")
	}
}

func TestConcurrentRequestLimitsDefaultsAndValidation(t *testing.T) {
	server, err := New(Config{Store: storage.NewMemoryStore(), Auth: DevBypass})
	if err != nil {
		t.Fatal(err)
	}
	if cap(server.requestSlots) != DefaultMaxConcurrentRequests {
		t.Fatal("unexpected request default")
	}
	if _, err := New(Config{Store: storage.NewMemoryStore(), Auth: DevBypass, MaxConcurrentRequests: -1}); err == nil {
		t.Fatal("accepted negative concurrency")
	}
}
