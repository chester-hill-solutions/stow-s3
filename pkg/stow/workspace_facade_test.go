package stow_test

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func TestWorkspaceFacadeSharesRuntimeAndRefreshesHostWrites(t *testing.T) {
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: t.TempDir(), MaxBytes: 8, MaxObjects: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	facade, err := ws.Facade()
	if err != nil {
		t.Fatal(err)
	}
	again, err := ws.Facade()
	if err != nil || *again != *facade {
		t.Fatalf("repeated facade: %v", err)
	}
	client := facadeS3(facade)
	ctx := context.Background()
	put := func(key, data string) error {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(ws.Bucket()), Key: aws.String(key), Body: strings.NewReader(data)})
		return err
	}
	if err := put("api", "1234"); err != nil {
		t.Fatal(err)
	}
	if got := ws.Usage(); got.Bytes != 4 || got.Objects != 1 {
		t.Fatalf("usage: %+v", got)
	}
	if err := os.WriteFile(filepath.Join(ws.Dir(), "host"), []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.PutObject(ctx, ws.Bucket(), "third", []byte("x"), stow.PutOptions{}); !errors.Is(err, stow.ErrQuotaExceeded) {
		t.Fatalf("host quota: %v", err)
	}
	if err := os.Remove(filepath.Join(ws.Dir(), "host")); err != nil {
		t.Fatal(err)
	}
	if err := put("third", "1234"); err != nil {
		t.Fatal(err)
	}
	checkResizedWorkspaceQuota(t, ws, put)
	response, err := http.Get(facade.Endpoint + "/" + ws.Bucket() + "/third")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("unauthenticated status %d", response.StatusCode)
	}
	checkClosedWorkspaceFacade(t, ws, put)
}

func TestWorkspaceFacadeConcurrentLifecycle(t *testing.T) {
	ws := openWorkspace(t, t.TempDir())
	var group sync.WaitGroup
	for range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := ws.Facade()
			if err != nil && !errors.Is(err, stow.ErrClosed) {
				t.Error(err)
			}
		}()
	}
	group.Add(1)
	go func() {
		defer group.Done()
		if err := ws.Close(); err != nil {
			t.Error(err)
		}
	}()
	group.Wait()
}

func TestPreparedWorkspaceCountsSeedUsage(t *testing.T) {
	source := filepath.Join(t.TempDir(), "seed")
	if err := os.WriteFile(source, []byte("1234"), 0600); err != nil {
		t.Fatal(err)
	}
	prepared, err := stow.PrepareWorkspace(stow.PrepareOptions{WorkspaceOptions: stow.WorkspaceOptions{Dir: filepath.Join(t.TempDir(), "workspace"), RegistryDir: t.TempDir(), MaxBytes: 4}, Inputs: []stow.WorkspaceInput{{Source: source, Destination: "seed"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Workspace.Close()
	if got := prepared.Workspace.Usage(); got.Bytes != 4 || got.Objects != 1 {
		t.Fatalf("seed usage: %+v", got)
	}
}

func checkResizedWorkspaceQuota(t *testing.T, ws *stow.Workspace, put func(string, string) error) {
	t.Helper()
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(ws.Dir(), "api"), []byte("123456"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := put("third", "123"); err == nil {
		t.Fatal("resized file escaped quota")
	}
	if err := ws.DeleteObject(ctx, ws.Bucket(), "api"); err != nil {
		t.Fatal(err)
	}
	if err := put("third", "12345678"); err != nil {
		t.Fatal(err)
	}
}

func checkClosedWorkspaceFacade(t *testing.T, ws *stow.Workspace, put func(string, string) error) {
	t.Helper()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Facade(); !errors.Is(err, stow.ErrClosed) {
		t.Fatalf("closed facade: %v", err)
	}
	if err := put("closed", "x"); err == nil {
		t.Fatal("closed facade accepted write")
	}
}

func TestWorkspaceFacadeReadOnlyAuthority(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input"), []byte("readable"), 0600); err != nil {
		t.Fatal(err)
	}
	authority := stow.ReadOnly()
	ws, err := stow.OpenWorkspace(stow.WorkspaceOptions{Dir: root, RegistryDir: t.TempDir(), Authority: &authority})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	facade, err := ws.Facade()
	if err != nil {
		t.Fatal(err)
	}
	client := facadeS3(facade)
	result, err := client.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String(ws.Bucket()), Key: aws.String("input")})
	if err != nil {
		t.Fatal(err)
	}
	result.Body.Close()
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(ws.Bucket()), Key: aws.String("output"), Body: strings.NewReader("denied")}); err == nil {
		t.Fatal("read-only facade accepted a write")
	}
}

func facadeS3(facade *stow.WorkspaceFacade) *s3.Client {
	return s3.New(s3.Options{Region: facade.Region, BaseEndpoint: aws.String(facade.Endpoint), UsePathStyle: true,
		Credentials: credentials.NewStaticCredentialsProvider(facade.AccessKeyID, facade.SecretAccessKey, "")})
}
