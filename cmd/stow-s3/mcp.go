package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/mcpstorage"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mcpCommand(args []string) error {
	if versionRequested(args) {
		printVersion(os.Stdout, true)
		return nil
	}
	flags := flag.NewFlagSet("mcp", flag.ContinueOnError)
	registry := flags.String("registry-dir", "", "Required workspace registry directory")
	id := flags.String("workspace-id", "", "Required allowed workspace ID")
	maxBytes := flags.Int64("max-bytes", 1<<30, "Maximum capture bytes")
	maxFiles := flags.Int64("max-files", 100000, "Maximum capture files")
	exportRoot := flags.String("export-root", "", "Existing transfer directory; enables bundle export")
	adoptRoot := flags.String("adopt-root", "", "Existing destination directory; enables bundle adoption")
	timeout := flags.Duration("timeout", 0, "Required positive capture deadline, for example 30s")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("mcp takes flags only")
	}
	server, err := mcpstorage.New(mcpstorage.Config{RegistryDir: *registry, WorkspaceID: *id, MaxBytes: *maxBytes, MaxFiles: *maxFiles, Timeout: time.Duration(*timeout), ExportRoot: *exportRoot, AdoptRoot: *adoptRoot})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return server.Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
}
