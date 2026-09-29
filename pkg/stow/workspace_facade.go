package stow

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
)

// WorkspaceFacade describes the loopback S3 endpoint owned by a Workspace.
type WorkspaceFacade struct {
	MaxMultipartUploads   int64  `json:"max_multipart_uploads"`
	MaxConcurrentRequests int    `json:"max_concurrent_requests"`
	MaxRequestBytes       int64  `json:"max_request_bytes"`
	Endpoint              string `json:"endpoint"`
	Region                string `json:"region"`
	AccessKeyID           string `json:"access_key_id"`
	SecretAccessKey       string `json:"secret_access_key"`
}

type workspaceServer struct {
	descriptor WorkspaceFacade
	server     *s3api.Server
	done       chan error
}

type borrowedWorkspaceStore struct{ *runtime.StoreAdapter }

func (*borrowedWorkspaceStore) Close() error { return nil }

// Facade starts one authenticated loopback server sharing this workspace's runtime.
type WorkspaceFacadeOptions struct {
	MaxRequestBytes       int64
	MaxConcurrentRequests int
}

func (w *Workspace) Facade() (*WorkspaceFacade, error) {
	return w.FacadeWithOptions(WorkspaceFacadeOptions{})
}

func (w *Workspace) FacadeWithOptions(options WorkspaceFacadeOptions) (*WorkspaceFacade, error) {
	if options.MaxConcurrentRequests < 0 {
		return nil, fmt.Errorf("stow: maximum concurrent requests must not be negative")
	}
	if options.MaxConcurrentRequests == 0 {
		options.MaxConcurrentRequests = s3api.DefaultMaxConcurrentRequests
	}
	if options.MaxRequestBytes < 0 {
		return nil, fmt.Errorf("stow: maximum request bytes must not be negative")
	}
	if options.MaxRequestBytes == 0 {
		options.MaxRequestBytes = s3api.DefaultMaxRequestBytes
	}
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if w.closed || w.closing {
		return nil, ErrClosed
	}
	if w.facade == nil {
		facade, err := startWorkspaceFacade(w.Runtime.inner, options)
		if err != nil {
			return nil, err
		}
		w.facade = facade
	}
	if w.facade.descriptor.MaxRequestBytes != options.MaxRequestBytes || w.facade.descriptor.MaxConcurrentRequests != options.MaxConcurrentRequests {
		return nil, fmt.Errorf("stow: existing workspace facade has a different request limit")
	}
	descriptor := w.facade.descriptor
	return &descriptor, nil
}

func startWorkspaceFacade(instance *runtime.Instance, options WorkspaceFacadeOptions) (*workspaceServer, error) {
	credentials, err := auth.GenerateCredentials()
	if err != nil {
		return nil, err
	}
	adapter, err := runtime.NewStoreAdapter(instance)
	if err != nil {
		return nil, err
	}
	const region = "us-east-1"
	server, err := s3api.New(s3api.Config{
		MaxRequestBytes: options.MaxRequestBytes, MaxConcurrentRequests: options.MaxConcurrentRequests,
		Store: &borrowedWorkspaceStore{adapter}, Host: "127.0.0.1", Region: region,
		Auth: s3api.SigV4Auth(auth.NewVerifier(region), credentials),
		Mode: "local", CachePolicy: "none", WritePolicy: "local-only",
		AdminToken: credentials.SecretAccessKey,
	})
	if err != nil {
		return nil, err
	}
	facade := &workspaceServer{server: server, done: make(chan error, 1)}
	go func() { facade.done <- server.ListenAndServe() }()
	<-server.Ready()
	if server.Addr() == "" {
		return nil, <-facade.done
	}
	facade.descriptor = WorkspaceFacade{MaxMultipartUploads: instance.Capabilities().MaxMultipartUploads, MaxRequestBytes: options.MaxRequestBytes, MaxConcurrentRequests: options.MaxConcurrentRequests, Endpoint: "http://" + server.Addr(), Region: region,
		AccessKeyID: credentials.AccessKeyID, SecretAccessKey: credentials.SecretAccessKey}
	return facade, nil
}

func (w *Workspace) closeRuntimeLocked() error {
	if w.closed {
		return nil
	}
	w.closing = true
	if w.facade != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := w.facade.server.Shutdown(ctx); err != nil {
			return err
		}
		done := w.facade.done
		w.facade = nil
		if err := <-done; err != nil && err != http.ErrServerClosed {
			return err
		}
	}
	if err := w.Runtime.Close(); err != nil {
		return err
	}
	return nil
}

func (w *Workspace) closeLocked() error {
	if err := w.closeRuntimeLocked(); err != nil {
		return err
	}
	if err := w.session.Release(); err != nil {
		return err
	}
	w.closed = true
	return nil
}

// RefreshUsage accounts for direct filesystem writes at a caller's quiescent boundary.
func (w *Workspace) RefreshUsage(ctx context.Context) error {
	return w.Runtime.inner.RefreshUsage(ctx)
}
