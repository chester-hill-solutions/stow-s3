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
	RegistryDir  string
	WorkspaceID  string
	CheckpointID string
	ReadRoots    []string
	MaxBytes     int64
	MaxFiles     int64
	// MaxFileBytes is the effective workspace editing bound; zero means the
	// default 64 KiB. Inspect reports this value so an agent can size a save
	// against the bound the serving process actually enforces.
	MaxFileBytes int64
	Timeout      time.Duration
	ExportRoot   string
	AdoptRoot    string
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
	if config.MaxFileBytes < 0 || config.MaxFileBytes > 1<<20 {
		return nil, fmt.Errorf("workspace file bound must be between 0 and 1048576")
	}
	if config.MaxFileBytes == 0 {
		config.MaxFileBytes = 65536
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
	mcp.AddTool(server, &mcp.Tool{Name: "stow_workspace_inspect", Title: "Inspect a workspace",
		Description: "Inspect the configured workspace: identity, working directory, bucket, limits, features and any file-save barrier. Use it before planning work, or when a call reports an unknown workspace state; it does not open or claim the workspace. Returns the same shape workspace prepare and resume print.",
		Annotations: readOnlyTool("Inspect a workspace")}, a.inspectWorkspace)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_create", Title: "Create a checkpoint",
		Description: "Capture the workspace's current files into a new checkpoint. Persist a random request key before calling and reuse it to reconcile a lost reply instead of capturing again. Stop known writers first: this is a quiescent capture, not a live snapshot. Returns outcome, checkpoint id, file and byte totals, and excluded paths.",
		Annotations: writeTool("Create a checkpoint", false)}, a.capture)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_resolve", Title: "Resolve a checkpoint capture",
		Description: "Return the result of a previous checkpoint capture using its original key and options, without capturing again. Use it after a timeout or an uncertain reply; a missing receipt is not proof that no checkpoint exists. Returns the same result with replayed true.",
		Annotations: readOnlyTool("Resolve a checkpoint capture")}, a.resolve)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_inspect", Title: "Inspect a checkpoint",
		Description: "Inspect bounded pages of a saved checkpoint's entries and excluded paths. Use it to answer what a checkpoint contains before restoring or handing it off; it does not read file contents. Returns one page plus a cursor.",
		Annotations: readOnlyTool("Inspect a checkpoint")}, a.inspectCheckpoint)
	mcp.AddTool(server, &mcp.Tool{Name: "stow_checkpoint_diff", Title: "Compare checkpoints",
		Description: "Compare two checkpoints in the configured workspace and report added, changed and deleted paths. Use it to summarise what a task changed; it does not compare working files against a checkpoint. Returns the diff document.",
		Annotations: readOnlyTool("Compare checkpoints")}, a.diff)
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
