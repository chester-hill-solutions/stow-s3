package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

type workspaceServeResult struct {
	workspaceResult
	*stow.WorkspaceFacade
}

func serveWorkspaceCommand(args []string) error {
	flags := flag.NewFlagSet("workspace serve", flag.ContinueOnError)
	maxConcurrentRequests := flags.Int("max-concurrent-requests", s3api.DefaultMaxConcurrentRequests, "Maximum concurrent HTTP requests; excess receive 503")
	maxRequestBytes := flags.Int64("max-request-bytes", s3api.DefaultMaxRequestBytes, "Maximum HTTP request body bytes; must be positive")
	id := flags.String("id", "", "Workspace ID to own and serve until shutdown")
	readyFD := flags.Int("ready-fd", -1, "Write readiness JSON to this file descriptor instead of stdout")
	parentPID := flags.Int("parent-pid", 0, "Stop if this parent process exits")
	maxBytes := flags.Int64("max-bytes", 0, "Workspace byte limit (0 uses default)")
	maxObjects := flags.Int64("max-objects", 0, "Workspace object limit (0 uses default)")
	chosen := registryFlag(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *id == "" {
		return errors.New("workspace serve requires --id")
	}
	if *maxRequestBytes <= 0 || *maxConcurrentRequests <= 0 {
		return errors.New("max-request-bytes and max-concurrent-requests must be positive")
	}
	if *maxBytes < 0 || *maxObjects < 0 || *readyFD < -1 || *parentPID < 0 {
		return errors.New("workspace serve flags must not be negative")
	}
	selection := chosen()
	registry, err := selection.resolve("")
	if err != nil {
		return err
	}
	if *parentPID > 0 {
		armParentWatch(*parentPID)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ws, err := stow.ResumeWith(stow.WorkspaceOptions{RegistryDir: registry, MaxBytes: *maxBytes, MaxObjects: *maxObjects}, *id)
	if err != nil {
		return err
	}
	defer ws.Close()
	facade, err := ws.FacadeWithOptions(stow.WorkspaceFacadeOptions{MaxRequestBytes: *maxRequestBytes, MaxConcurrentRequests: *maxConcurrentRequests})
	if err != nil {
		return err
	}
	result := workspaceServeResult{workspaceResult: makeWorkspaceResult(ws, 0, 0), WorkspaceFacade: facade}
	result.RegistryDir, result.Team = registry, selection.team
	if err := writeWorkspaceReady(*readyFD, result); err != nil {
		return err
	}
	<-ctx.Done()
	return ws.Close()
}

func writeWorkspaceReady(fd int, result workspaceServeResult) error {
	if fd < 0 {
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	file := os.NewFile(uintptr(fd), "workspace-ready")
	if file == nil {
		return errors.New("invalid readiness file descriptor")
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(result)
}
