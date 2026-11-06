package s3api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

type lifecycleStore struct {
	*storage.MemoryStore
	closes atomic.Int32
}

func (s *lifecycleStore) Close() error {
	s.closes.Add(1)
	return s.MemoryStore.Close()
}

func newLifecycleServer(t *testing.T) (*Server, *lifecycleStore) {
	t.Helper()
	store := &lifecycleStore{MemoryStore: storage.NewMemoryStore()}
	server, err := New(Config{Store: store, Auth: DevBypass, Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return server, store
}

func TestServerBindFailureIsStoppedUntilShutdown(t *testing.T) {
	server, store := newLifecycleServer(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server.config.Port = listener.Addr().(*net.TCPAddr).Port
	if err := server.ListenAndServe(); err == nil {
		t.Fatal("bound an occupied port")
	}
	<-server.Ready()
	if server.lifecycle != serverStopped || store.closes.Load() != 0 {
		t.Fatal("bind failure must stop startup without closing the store")
	}
	if err := server.ListenAndServe(); err == nil || errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("restart after bind failure = %v", err)
	}
	for range 2 {
		if err := server.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if server.lifecycle != serverClosed || store.closes.Load() != 1 {
		t.Fatal("shutdown must close the store exactly once")
	}
}

func TestServerInterruptedShutdownWaitsForStartupOnRetry(t *testing.T) {
	server, store := newLifecycleServer(t)
	// Hold the startup phase at the point before net.Listen completes.
	if err := server.beginServe(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted shutdown = %v", err)
	}
	if server.lifecycle != serverStopping || store.closes.Load() != 0 {
		t.Fatal("shutdown closed storage before startup settled")
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("startup during shutdown = %v", err)
	}
	select {
	case <-server.Ready():
		t.Fatal("rejected startup released the original startup barrier")
	default:
	}
	server.finishServe()
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.finishServe()
	if server.lifecycle != serverClosed || store.closes.Load() != 1 {
		t.Fatal("late startup completion changed a closed server")
	}
}

func TestServerServeExitCannotRestart(t *testing.T) {
	server, _ := newLifecycleServer(t)
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	<-server.Ready()
	server.mu.RLock()
	phase, listener := server.lifecycle, server.listener
	server.mu.RUnlock()
	if phase != serverServing || listener == nil {
		t.Fatalf("startup phase = %v, listener = %v", phase, listener)
	}
	if err := server.ListenAndServe(); err == nil {
		t.Fatal("started a second listener")
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, net.ErrClosed) {
		t.Fatalf("serve exit = %v", err)
	}
	if server.lifecycle != serverStopped {
		t.Fatalf("serve exit phase = %v", server.lifecycle)
	}
	if err := server.ListenAndServe(); err == nil {
		t.Fatal("restarted after serve exited")
	}
}

func TestServerShutdownBeforeStartupSignalsReady(t *testing.T) {
	server, _ := newLifecycleServer(t)
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.Ready():
	default:
		t.Fatal("shutdown left readiness waiters blocked")
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("startup after shutdown = %v", err)
	}
}

func TestInterruptedShutdownRetainsStoreUntilRequestsDrain(t *testing.T) {
	server, store := newLifecycleServer(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseRequest := sync.OnceFunc(func() { close(release) })
	defer releaseRequest()
	server.auth = func(*http.Request) error {
		close(entered)
		<-release
		return nil
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ListenAndServe() }()
	<-server.Ready()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://"+server.Addr()+"/assets", nil)
	if err != nil {
		t.Fatal(err)
	}
	requestDone := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				err = errors.New("in-flight request failed during shutdown")
			}
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("request never entered server")
	}
	interrupted, stop := context.WithCancel(context.Background())
	stop()
	if err := server.Shutdown(interrupted); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted shutdown = %v", err)
	}
	if store.closes.Load() != 0 {
		t.Fatal("closed storage while an in-flight request still owned it")
	}
	releaseRequest()
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve = %v", err)
	}
	if store.closes.Load() != 1 {
		t.Fatal("successful retry did not close storage exactly once")
	}
}
