package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/chester-hill-solutions/stow-s3/internal/mcpstorage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpCommandOptions struct {
	workspace mcpstorage.Config
	objects   mcpstorage.ObjectConfig

	// The HTTP transport and its exposure decision. Empty httpAddr keeps stdio.
	httpAddr            string
	token               string
	allowInsecureRemote bool
}

func mcpCommand(args []string) error {
	if versionRequested(args) {
		printVersion(os.Stdout, true)
		return nil
	}
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	options := registerMCPFlags(flags)
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("mcp takes flags only")
	}
	objectMode, err := validateMCPMode(flags)
	if err != nil {
		return err
	}
	if options.token == "" {
		options.token = strings.TrimSpace(os.Getenv("STOW_MCP_TOKEN"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server, closer, err := mcpServerFor(objectMode, options)
	if err != nil {
		return err
	}
	defer func() { _ = closer() }()
	if options.httpAddr != "" {
		if err := validateMCPExposure(options.httpAddr, options.token, options.allowInsecureRemote); err != nil {
			return err
		}
		return runMCPHTTP(ctx, server, options.httpAddr, options.token)
	}
	return server.Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
}

func registerMCPFlags(flags *flag.FlagSet) *mcpCommandOptions {
	options := &mcpCommandOptions{}
	flags.StringVar(&options.workspace.RegistryDir, "registry-dir", "", "Workspace registry directory")
	flags.StringVar(&options.workspace.WorkspaceID, "workspace-id", "", "Allowed workspace ID")
	flags.Int64Var(&options.workspace.MaxBytes, "max-bytes", 1<<30, "Maximum capture bytes or object-store bytes")
	flags.Int64Var(&options.workspace.MaxFiles, "max-files", 100000, "Maximum capture files (workspace mode)")
	flags.StringVar(&options.workspace.ExportRoot, "export-root", "", "Existing transfer directory; enables workspace bundle export")
	flags.StringVar(&options.workspace.AdoptRoot, "adopt-root", "", "Existing destination directory; enables workspace bundle adoption")
	flags.DurationVar(&options.workspace.Timeout, "timeout", 0, "Required positive workspace capture deadline, for example 30s")
	flags.StringVar(&options.objects.Dir, "object-dir", "", "Owned native object-store directory; selects object mode")
	flags.StringVar(&options.objects.Bucket, "bucket", "", "Exact allowed bucket (object mode)")
	flags.Int64Var(&options.objects.MaxObjects, "max-objects", 10000, "Maximum stored objects (object mode)")
	flags.IntVar(&options.objects.MaxObjectBytes, "max-object-bytes", 65536, "Maximum read/save body bytes, up to 65536 (object mode)")
	flags.IntVar(&options.objects.MaxObservations, "max-observations", 256, "Maximum retained observations, up to 256 (object mode)")
	flags.BoolVar(&options.objects.CreateBucket, "create-bucket", false, "Explicitly create the configured bucket if absent (object mode)")
	flags.BoolVar(&options.objects.ReadOnly, "read-only", false, "Refuse object saves and bucket creation (object mode)")
	flags.BoolVar(&options.objects.Browser, "browser", false, "Enable the read-only MCP App browser and resource mentions (object mode)")
	flags.StringVar(&options.httpAddr, "http-addr", "", "Serve MCP over streamable HTTP at host:port instead of stdio")
	flags.StringVar(&options.token, "mcp-token", "", "Bearer token required from callers that are not loopback; prefer STOW_MCP_TOKEN")
	flags.BoolVar(&options.allowInsecureRemote, "allow-insecure-remote", false, "Permit an HTTP address beyond loopback; requires TLS in front of this process")
	return options
}

func validateMCPMode(flags *flag.FlagSet) (bool, error) {
	visited := make(map[string]bool)
	flags.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	objectMode := visited["object-dir"] || visited["bucket"]
	disallowed := []string{"object-dir", "bucket", "max-objects", "max-object-bytes", "max-observations", "create-bucket", "read-only", "browser"}
	if objectMode {
		disallowed = []string{"registry-dir", "workspace-id", "max-files", "export-root", "adopt-root", "timeout"}
	}
	for _, name := range disallowed {
		if visited[name] {
			return false, fmt.Errorf("mcp flag --%s is incompatible with the selected storage mode", name)
		}
	}
	return objectMode, nil
}

// mcpServerFor builds the surface for the selected mode. Both transports share it,
// so a tool is reachable the same way whether it is served over stdio or HTTP.
func mcpServerFor(objectMode bool, options *mcpCommandOptions) (*mcp.Server, func() error, error) {
	if objectMode {
		options.objects.MaxBytes = options.workspace.MaxBytes
		objects, err := mcpstorage.NewObjects(options.objects)
		if err != nil {
			return nil, nil, err
		}
		return objects.Server, objects.Close, nil
	}
	server, err := mcpstorage.New(options.workspace)
	if err != nil {
		return nil, nil, err
	}
	return server, func() error { return nil }, nil
}
