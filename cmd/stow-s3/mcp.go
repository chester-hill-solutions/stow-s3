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
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpCommandOptions struct {
	workspace mcpstorage.Config
	objects   mcpstorage.ObjectConfig
	readRoots repeatedStringFlag

	// The HTTP transport and its exposure decision. Empty httpAddr keeps stdio.
	httpAddr            string
	token               string
	allowInsecureRemote bool
	workspaceFiles      bool
	resourcesOnly       bool
	workspaceEdit       bool
	writeRoots          repeatedStringFlag
	coordination        string
	recoverFileSaves    bool
}

func mcpCommand(args []string) (err error) {
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
	options.workspace.ReadRoots = append(options.workspace.ReadRoots, options.readRoots...)
	if options.token != "" && strings.TrimSpace(options.token) == "" {
		return fmt.Errorf("--mcp-token must not contain only whitespace")
	}
	options.token = strings.TrimSpace(options.token)
	if options.token == "" {
		options.token = strings.TrimSpace(os.Getenv("STOW_MCP_TOKEN"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server, closer, err := mcpServerFor(objectMode, options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closer()) }()
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
	flags.StringVar(&options.workspace.CheckpointID, "checkpoint-id", "", "Select one saved checkpoint file view (workspace-files mode)")
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
	flags.BoolVar(&options.objects.Browser, "browser", false, "Enable the read-only MCP App browser (object or workspace file mode)")
	flags.StringVar(&options.httpAddr, "http-addr", "", "Serve MCP over streamable HTTP at host:port instead of stdio")
	flags.StringVar(&options.token, "mcp-token", "", "Bearer token required from callers that are not loopback; prefer STOW_MCP_TOKEN")
	flags.BoolVar(&options.allowInsecureRemote, "allow-insecure-remote", false, "Permit an HTTP address beyond loopback; requires TLS in front of this process")
	flags.BoolVar(&options.workspaceFiles, "workspace-files", false, "Expose bounded read-only resources for the configured workspace")
	flags.BoolVar(&options.resourcesOnly, "resources-only", false, "Expose workspace file resources without checkpoint tools")
	flags.Var(&options.readRoots, "read-root", "Limit workspace file resources to this existing relative directory; repeatable")
	flags.BoolVar(&options.workspaceEdit, "workspace-edit", false, "Enable guarded file editing for the configured workspace")
	flags.Var(&options.writeRoots, "write-root", "Limit workspace file editing to this existing relative directory; repeatable")
	flags.StringVar(&options.coordination, "coordination", "", "Workspace edit coordination; requires host_quiesced")
	flags.BoolVar(&options.recoverFileSaves, "recover-file-saves", false, "Open workspace file-save recovery state; requires --workspace-edit")
	return options
}

type repeatedStringFlag []string

func (values *repeatedStringFlag) String() string { return strings.Join(*values, ",") }

func (values *repeatedStringFlag) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func validateMCPMode(flags *flag.FlagSet) (bool, error) {
	visited := make(map[string]bool)
	flags.Visit(func(item *flag.Flag) { visited[item.Name] = true })
	objectMode := visited["object-dir"] || visited["bucket"]
	mode := mcpModeValidation{
		objectMode: objectMode, workspaceFiles: flags.Lookup("workspace-files").Value.String() == "true",
		resourcesOnly: flags.Lookup("resources-only").Value.String() == "true", checkpointSelected: visited["checkpoint-id"],
		workspaceEdit: flags.Lookup("workspace-edit").Value.String() == "true",
	}
	if err := validateMCPStorageFlags(flags, visited, objectMode); err != nil {
		return false, err
	}
	if err := validateWorkspaceMCPFlags(flags, visited, mode); err != nil {
		return false, err
	}
	return objectMode, nil
}

type mcpModeValidation struct {
	objectMode         bool
	workspaceFiles     bool
	resourcesOnly      bool
	checkpointSelected bool
	workspaceEdit      bool
}

func validateMCPStorageFlags(flags *flag.FlagSet, visited map[string]bool, objectMode bool) error {
	if flags.Lookup("resources-only").Value.String() == "true" && flags.Lookup("workspace-files").Value.String() != "true" {
		return fmt.Errorf("--resources-only requires --workspace-files")
	}
	disallowed := []string{"object-dir", "bucket", "max-objects", "max-object-bytes", "max-observations", "create-bucket", "read-only"}
	if objectMode {
		disallowed = []string{"registry-dir", "workspace-id", "checkpoint-id", "max-files", "export-root", "adopt-root", "timeout", "workspace-files", "resources-only", "read-root", "workspace-edit", "write-root", "coordination", "recover-file-saves"}
	}
	for _, name := range disallowed {
		if visited[name] {
			return fmt.Errorf("mcp flag --%s is incompatible with the selected storage mode", name)
		}
	}
	return nil
}

func validateWorkspaceMCPFlags(flags *flag.FlagSet, visited map[string]bool, mode mcpModeValidation) error {
	if mode.objectMode {
		return nil
	}
	if err := validateWorkspaceEditFlags(flags, visited, mode); err != nil {
		return err
	}
	if mode.checkpointSelected && !mode.workspaceFiles {
		return fmt.Errorf("--checkpoint-id requires --workspace-files")
	}
	if mode.checkpointSelected && flags.Lookup("checkpoint-id").Value.String() == "" {
		return fmt.Errorf("--checkpoint-id requires a non-empty checkpoint ID")
	}
	if visited["read-root"] && !mode.workspaceFiles {
		return fmt.Errorf("--read-root requires --workspace-files")
	}
	if flags.Lookup("browser").Value.String() == "true" {
		if !mode.workspaceFiles {
			return fmt.Errorf("--browser requires --workspace-files outside object mode")
		}
		if mode.resourcesOnly {
			return fmt.Errorf("--browser is incompatible with --resources-only")
		}
		if mode.checkpointSelected {
			return fmt.Errorf("--browser is incompatible with --checkpoint-id")
		}
	}
	return nil
}

func validateWorkspaceEditFlags(flags *flag.FlagSet, visited map[string]bool, mode mcpModeValidation) error {
	editOptions := []string{"write-root", "coordination", "recover-file-saves"}
	if !mode.workspaceEdit {
		for _, name := range editOptions {
			if visited[name] {
				return fmt.Errorf("--%s requires --workspace-edit", name)
			}
		}
		return nil
	}
	if !mode.workspaceFiles || mode.resourcesOnly || mode.checkpointSelected || flags.Lookup("browser").Value.String() == "true" {
		return fmt.Errorf("--workspace-edit requires mutable workspace file mode without browser or checkpoint selection")
	}
	options := parseWorkspaceEditFlags(flags)
	if len(options.writeRoots) == 0 || options.coordination != "host_quiesced" {
		return fmt.Errorf("--workspace-edit requires --write-root and --coordination host_quiesced")
	}
	for _, root := range options.writeRoots {
		if root == "" {
			return fmt.Errorf("--write-root must not be empty")
		}
	}
	return nil
}

type workspaceEditFlags struct {
	writeRoots   []string
	coordination string
}

func parseWorkspaceEditFlags(flags *flag.FlagSet) workspaceEditFlags {
	writeRoots, _ := flags.Lookup("write-root").Value.(*repeatedStringFlag)
	return workspaceEditFlags{writeRoots: append([]string(nil), (*writeRoots)...),
		coordination: flags.Lookup("coordination").Value.String()}
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
	if options.workspaceEdit {
		files, err := mcpstorage.NewWorkspaceEditing(options.workspace, stow.WorkspaceEditOptions{
			WriteRoots: append([]string(nil), options.writeRoots...), Coordination: options.coordination,
		}, options.recoverFileSaves)
		if err != nil {
			return nil, nil, err
		}
		return files.Server, files.Close, nil
	}
	if options.workspaceFiles {
		files, err := mcpstorage.NewWorkspaceFiles(options.workspace, options.resourcesOnly)
		if err != nil {
			return nil, nil, err
		}
		if options.objects.Browser {
			if err := files.EnableBrowser(); err != nil {
				_ = files.Close()
				return nil, nil, err
			}
		}
		return files.Server, files.Close, nil
	}
	server, err := mcpstorage.New(options.workspace)
	if err != nil {
		return nil, nil, err
	}
	return server, func() error { return nil }, nil
}
