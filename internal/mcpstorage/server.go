package mcpstorage

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const Guide = `Stow saves working data; your host owns execution and writer coordination.
Before saving, wait for tools and known background writers, then write objective,
progress and remaining work to an ordinary file in the workspace. Generate a random
request_key and keep the same key and options when reconciling an uncertain reply.
Only report a save after outcome=committed. On failure, report the last confirmed
checkpoint and hold the next turn. Resolve an uncertain request before retrying.
Retries must be bounded by a deadline; never rerun the agent prompt as a storage retry.
Agent-requested saves are best effort. Reliable turn saves require a host-controlled
admission barrier. Checkpoints exclude private state and common sensitive paths;
inspect exclusions. A saved checkpoint does not imply task success or passing tests.
Full chat state, process memory, credentials and remote side effects are not restored.`

type Config struct {
	RegistryDir string
	WorkspaceID string
	MaxBytes    int64
	MaxFiles    int64
	Timeout     time.Duration
	ExportRoot  string
	AdoptRoot   string
}

type adapter struct {
	config  Config
	mu      sync.RWMutex
	adopted map[string]bool
}

func New(config Config) (*mcp.Server, error) {
	if config.RegistryDir == "" || config.WorkspaceID == "" {
		return nil, fmt.Errorf("MCP requires an explicit registry and workspace ID")
	}
	if config.MaxBytes <= 0 || config.MaxFiles <= 0 || config.Timeout <= 0 {
		return nil, fmt.Errorf("MCP requires positive capture bounds and timeout")
	}
	dir, err := filepath.Abs(config.RegistryDir)
	if err != nil {
		return nil, err
	}
	config.RegistryDir = dir
	if _, err := stow.LookupWorkspace(dir, config.WorkspaceID); err != nil {
		return nil, err
	}
	a := &adapter{config: config, adopted: make(map[string]bool)}
	if err := a.configureRoots(); err != nil {
		return nil, err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "stow-storage", Version: "1"}, &mcp.ServerOptions{Instructions: Guide})
	mcp.AddTool(server, &mcp.Tool{Name: "stow_workspace_inspect", Description: "Inspect the configured workspace without opening or claiming it."}, a.inspectWorkspace)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_create", Description: "Capture quiescent working data. Persist a random request key before calling; reuse it to reconcile a lost reply."}, a.capture)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_resolve", Description: "Resolve a previous capture using its original key and options without creating a new checkpoint."}, a.resolve)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_inspect", Description: "Inspect bounded pages of saved checkpoint entries and exclusions."}, a.inspectCheckpoint)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_diff", Description: "Compare two checkpoints in the configured workspace."}, a.diff)
	a.addHandoffTools(server)
	server.AddResource(&mcp.Resource{URI: "stow://guides/checkpoints/v1", Name: "Checkpoint workflow", MIMEType: "text/plain"}, func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "stow://guides/checkpoints/v1", MIMEType: "text/plain", Text: Guide}}}, nil
	})
	return server, nil
}

func (a *adapter) scope(id string) error {
	a.mu.RLock()
	allowed := a.adopted[id]
	a.mu.RUnlock()
	if id != a.config.WorkspaceID && !allowed {
		return fmt.Errorf("workspace is outside the configured scope")
	}
	return nil
}

func (a *adapter) checkpoint(id string) (stow.CheckpointManifest, error) {
	cp, err := stow.LoadCheckpoint(a.config.RegistryDir, id)
	if err != nil {
		return cp, err
	}
	return cp, a.scope(cp.WorkspaceID)
}
