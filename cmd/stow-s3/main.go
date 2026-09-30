package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/chester-hill-solutions/stow-s3/internal/auth"
	"github.com/chester-hill-solutions/stow-s3/internal/authority"
	"github.com/chester-hill-solutions/stow-s3/internal/parentwatch"
	"github.com/chester-hill-solutions/stow-s3/internal/ready"
	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
	"github.com/chester-hill-solutions/stow-s3/internal/runtime"
	"github.com/chester-hill-solutions/stow-s3/internal/s3api"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/internal/storage/fs"
	"github.com/chester-hill-solutions/stow-s3/internal/version"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// readyDetails is what the readiness announcement needs to describe the server.
type readyDetails struct {
	maxConcurrentRequests int
	maxRequestBytes       int64
	endpoint              string
	mode                  string
	backend               runtime.Backend
	capabilities          runtime.Capabilities
	limits                nativeStorageLimits
	creds                 auth.Credentials
	banner                string
	// region is the region the server actually verifies signatures against, so
	// a client that honors the reported region signs correctly.
	region string
}

// announceStartup prints the human-readable startup output and then publishes
// readiness. With a ready descriptor the versioned JSON object is written to it
// and the legacy STOW_READY line is deliberately not printed, so credentials
// stay off stdout for a client that asked for the versioned channel.
func announceStartup(readyFd int, details readyDetails) {
	fmt.Print(details.banner)
	fmt.Printf("stow listening on %s (mode=%s)\n", details.endpoint, details.mode)
	if readyFd >= 0 {
		writeReadyMessage(readyFd, details)
		return
	}
	fmt.Printf(
		"STOW_READY endpoint=%s access_key=%s secret_key=%s mode=%s\n",
		details.endpoint,
		details.creds.AccessKeyID,
		details.creds.SecretAccessKey,
		details.mode,
	)
}

func writeReadyMessage(fd int, details readyDetails) {
	if err := ready.WriteToFd(fd, readyMessage(details)); err != nil {
		log.Fatalf("write ready message: %v", err)
	}
}

func readyMessage(details readyDetails) ready.Message {
	return ready.New(ready.Input{
		Endpoint:      details.endpoint,
		Region:        details.region,
		AccessKeyID:   details.creds.AccessKeyID,
		SecretKey:     details.creds.SecretAccessKey,
		Mode:          details.mode,
		Backend:       string(details.backend),
		BinaryVersion: version.Version,
		Capabilities: ready.Capabilities{
			// The runtime's own answers, not a re-derivation: a capability
			// published here and a capability the runtime reports are two facts
			// that have to agree, and only one of them is measured.
			Persistent:            details.capabilities.Persistent,
			Multipart:             details.capabilities.Multipart,
			Upstream:              details.mode == string(runthrough.ModeRunThrough),
			ConditionalWrites:     true,
			PresignedURLs:         true,
			MaxBytes:              ready.ReportedLimit(details.limits.bytes()),
			MaxObjects:            ready.ReportedLimit(details.limits.objects()),
			MaxMultipartUploads:   details.capabilities.MaxMultipartUploads,
			MaxRequestBytes:       reportedRequestLimit(details.maxRequestBytes),
			MaxConcurrentRequests: reportedConcurrencyLimit(details.maxConcurrentRequests),
		},
	})
}

// parseBackend is the one place the --backend flag becomes a typed backend, so an
// unrecognised value is refused here rather than at whichever consumer happened
// to notice it.
func parseBackend(value string) (runtime.Backend, error) {
	switch backend := runtime.Backend(strings.ToLower(strings.TrimSpace(value))); backend {
	case runtime.BackendMemory, runtime.BackendFilesystem:
		return backend, nil
	case runtime.BackendWorkspace:
		// Reachable as a value but not constructible here: the workspace backend
		// needs options this command does not carry, and guessing at them would
		// hand a caller a directory in a shape they did not ask for. See
		// docs/architecture/environment-implementation.md M1.2.
		return "", fmt.Errorf("--backend %q is not selectable from the CLI yet; use the embedded API", value)
	default:
		return "", fmt.Errorf("invalid --backend %q (want filesystem or memory)", value)
	}
}

// openLocalStore creates the authoritative local store for the selected
// backend. Backend selection is a startup decision, so an unknown value or a
// failed open is fatal rather than an error a caller can recover from.
func openLocalStore(backend runtime.Backend, dataDir string) storage.Store {
	switch backend {
	case runtime.BackendFilesystem:
		store, err := fs.NewFilesystemStore(dataDir)
		if err != nil {
			log.Fatalf("open store: %v", err)
		}
		return store
	case runtime.BackendMemory:
		return storage.NewMemoryStore()
	default:
		log.Fatalf("no local store for backend %q", backend)
		return nil
	}
}

// armParentWatch installs the opt-in parent-death watch.
//
// Only a session passes a parent pid, so a hand-run server keeps its existing
// behavior of surviving its shell. A pid that is not this process's parent is
// fatal rather than watched, because watching a stranger would leave the server
// serving with no safety net while appearing protected. A platform with no
// mechanism is a missing safety net, not a reason to refuse to serve, but it
// must never be silent.
func armParentWatch(pid int) {
	request := parentwatch.Requested{Pid: pid, Getppid: os.Getppid}
	switch err := parentwatch.Watch(request); {
	case err == nil:
		log.Printf("will exit when parent process %d dies", pid)
	case errors.Is(err, parentwatch.ErrNotParent):
		log.Fatalf("refusing to start: %v", err)
	default:
		log.Printf("WARNING: parent-death watch unavailable: %v", err)
	}
}

func main() {
	// Ahead of the no-argument check: someone asking what they have is not asking
	// how to use it.
	if topLevelVersion(os.Args[1:]) {
		return
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		serve(os.Args[2:])
	case "doctor":
		doctor(os.Args[2:])
	case "mcp":
		if err := mcpCommand(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "workspace":
		if err := workspaceCommand(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "prewarm":
		if err := prewarm(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
		os.Exit(1)
	}
}

// usageTo takes a writer so a test can assert on it. A usage text no test can
// read silently loses lines.
func usageTo(out io.Writer) {
	fmt.Fprintf(out, "usage: stow-s3 <command>\n\ncommands:\n  serve       start the S3-compatible server\n  doctor      report whether this machine can run a stow-s3 session\n  workspace   prepare, resume, and hand off agent workspaces\n  mcp         expose scoped storage tools over stdio\n  prewarm     fetch named keys into the run-through cache so they survive the network\n\n")
	// prewarm takes an explicit key list, and saying so here is the documentation
	// that matters: a prefix would be the obvious thing to reach for, and it is a
	// data-exfiltration shape on a bucket an agent was never given.
	fmt.Fprintf(out, "warming a cache: stow-s3 prewarm --bucket B --keys k1,k2 (exact keys only, no prefixes)\n")
	// A caller that has just failed to find a health endpoint needs to be told where
	// it is, in the one place it will look. It is /_stow/health: every other path is
	// an S3 path that requires signing, so a bare probe of /health is refused and
	// looks like a server that is not serving.
	fmt.Fprintf(out, "probing a server: curl -s http://127.0.0.1:<port>/_stow/health\n")
	fmt.Fprintf(out, "stow-s3 doctor does that and more, on a throwaway port\n")
	fmt.Fprintf(out, "writing a launcher: docs/running-and-probing.md (readiness record, port and mode traps)\n")
	// Stated here as well as behind a flag, because this is where someone lands who
	// has just been told to check it.
	fmt.Fprintf(out, "checking a version: stow-s3 --version (every subcommand answers it too)\n")
}

func usage() { usageTo(os.Stderr) }

func resolveLocalCredentials(accessKey, secretKey string) (string, string) {
	if strings.TrimSpace(accessKey) == "" {
		accessKey = os.Getenv("STOW_LOCAL_ACCESS_KEY_ID")
	}
	if strings.TrimSpace(secretKey) == "" {
		secretKey = os.Getenv("STOW_LOCAL_SECRET_ACCESS_KEY")
	}
	return accessKey, secretKey
}

func applyCacheLimits(config *runthrough.Config, maxBytes, maxObjects int64, ttl time.Duration) error {
	if maxBytes < -1 || maxObjects < -1 || ttl < -1 {
		return fmt.Errorf("cache limits must not be negative")
	}
	if maxBytes >= 0 {
		config.Cache.MaxBytes = maxBytes
	}
	if maxObjects >= 0 {
		config.Cache.MaxObjects = maxObjects
	}
	if ttl >= 0 {
		config.Cache.TTL = ttl
	}
	return nil
}

// resolveMode combines the flag with the environment into one mode.
//
// The flag is the explicit request and it wins; an empty flag means nothing was
// asked for, and DetectMode's answer is local unless the environment named
// run-through. An empty default rather than "auto" is deliberate: "auto" would
// suggest that something else can select run-through, and what does is the
// machine's ambient AWS configuration.
func resolveMode(flagValue string) runthrough.Mode {
	switch strings.ToLower(strings.TrimSpace(flagValue)) {
	case "local":
		return runthrough.ModeLocal
	case "run-through":
		return runthrough.ModeRunThrough
	case "", "auto":
		return runthrough.DetectMode()
	default:
		log.Fatalf("invalid --mode %q (want local or run-through)", flagValue)
		return runthrough.ModeLocal
	}
}

func validateLiveWriteBackend(mode runthrough.Mode, backend runtime.Backend, config runthrough.Config) error {
	// Keyed on consent, not policy. A mirrorWrites policy without
	// STOW_ALLOW_LIVE_WRITES keeps every write local, so it is perfectly
	// serviceable from the memory backend and must not be refused here.
	if mode == runthrough.ModeRunThrough && backend == runtime.BackendMemory && runthrough.PropagatesUpstream(config) {
		return fmt.Errorf("run-through live writes require the filesystem backend")
	}
	return nil
}

func startOutboxRetryWorker(adapter *runthrough.Adapter) (context.CancelFunc, <-chan struct{}) {
	if adapter == nil {
		done := make(chan struct{})
		close(done)
		return func() {}, done
	}
	retryCtx, retryCancel := context.WithCancel(context.Background())
	retryDone := make(chan struct{})
	go func() {
		defer close(retryDone)
		_ = adapter.RetryPending(retryCtx)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-retryCtx.Done():
				return
			case <-ticker.C:
				_ = adapter.RetryPending(retryCtx)
			}
		}
	}()
	return retryCancel, retryDone
}

func serve(args []string) {
	opts := parseServeFlags(args)
	// Answered before anything is built, so --version does not need a writable data
	// directory, an authority, or an upstream to exist. See parseServeFlags.
	if opts.showVersion {
		printVersion(os.Stdout, true)
		return
	}
	if !opts.validRequestLimits() {
		log.Fatal("max-request-bytes and max-concurrent-requests must be positive")
	}
	rtCfg := serveRunThroughConfig(serveConfigInput{
		AccessKey: &opts.accessKey, SecretKey: &opts.secretKey,
		MaxBytes: opts.cacheMaxBytes, MaxObjects: opts.cacheMaxObjects, TTL: opts.cacheTTL,
		Offline: opts.offline, Mode: opts.modeFlag, AdminToken: &opts.adminToken,
		AllowLiveWrites: opts.allowLiveWrites, AllowPublicAdmin: opts.allowPublicAdmin,
		CacheDir: opts.cacheDir,
	})
	rtCfg.Upstream.AllowInsecureHTTP = opts.allowInsecureUpstream
	mode := resolveMode(opts.modeFlag)
	backend, err := parseBackend(opts.backendFlag)
	if err != nil {
		log.Fatal(err)
	}
	// The environment's authority, decided once here and enforced below every
	// interface. A nil pointer permits everything, which is what this server has
	// always done. One value, two consumers: the run-through adapter gates upstream
	// on it, the runtime instance gates everything else, and both are built from
	// this. See ADR 0010 decision 2.
	var granted *authority.Authority
	if opts.readOnly {
		narrowed := authority.ReadOnly()
		granted = &narrowed
		log.Printf("read-only: writes, deletes, bucket changes and upstream access are refused")
	}
	// Before buildStore, which constructs the run-through adapter.
	rtCfg.Authority = granted
	if err := validateLiveWriteBackend(mode, backend, rtCfg); err != nil {
		log.Fatal(err)
	}

	localDataDir := opts.dataDir
	if mode == runthrough.ModeRunThrough {
		if rtCfg.CacheDir == "" {
			rtCfg.CacheDir = filepath.Join(localDataDir, "cache")
		}
		if rtCfg.Upstream.Endpoint == "" || rtCfg.Upstream.AccessKey == "" || rtCfg.Upstream.SecretKey == "" {
			log.Fatal("run-through mode requires upstream credentials (set STOW_*, S3_*, or AWS_* env vars)")
		}
	}

	store, adapter, err := buildStore(mode, backend, localDataDir, rtCfg)
	if err != nil {
		log.Fatal(err)
	}

	storeLimits := nativeStorageLimits{maxBytes: opts.maxBytes, maxObjects: opts.maxObjects}
	boundStore, capabilities, err := bindNativeRuntimeStore(store, backend, adapter, storeLimits, granted)
	if err != nil {
		log.Fatal(err)
	}
	store = boundStore

	retryCancel, retryDone := startOutboxRetryWorker(adapter)
	defer retryCancel()

	creds := auth.Credentials{}
	if opts.accessKey != "" && opts.secretKey != "" {
		creds.AccessKeyID = opts.accessKey
		creds.SecretAccessKey = opts.secretKey
	} else {
		generated, err := auth.GenerateCredentials()
		if err != nil {
			log.Fatalf("generate credentials: %v", err)
		}
		creds = generated
	}

	region := serveRegion(opts.regionFlag)
	verifier := auth.NewVerifier(region)
	writePolicy := runthrough.EffectiveWritePolicy(rtCfg)
	cachePolicy := "none"
	upstreamHost := ""
	if mode == runthrough.ModeRunThrough {
		cachePolicy = string(rtCfg.Policy)
		upstreamHost = runthrough.RedactEndpoint(rtCfg.Upstream.Endpoint)
	}
	srv, err := s3api.New(s3api.Config{
		MaxConcurrentRequests: opts.maxConcurrentRequests,
		MaxRequestBytes:       opts.maxRequestBytes,
		Store:                 store,
		Auth:                  s3api.SigV4Auth(verifier, creds),
		Host:                  opts.host,
		BaseHost:              strings.TrimSpace(opts.baseHost),
		Port:                  opts.port,
		DataDir:               localDataDir,
		Region:                region,
		Mode:                  string(mode),
		CachePolicy:           cachePolicy,
		WritePolicy:           writePolicy,
		UpstreamHost:          upstreamHost,
		AllowPublicAdmin:      opts.allowPublicAdmin,
		AdminToken:            opts.adminToken,
		CORSOrigins:           opts.corsOrigins,
	})
	if err != nil {
		log.Fatalf("create server: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	deadline := time.Now().Add(2 * time.Second)
	for srv.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	addr := srv.Addr()
	if addr == "" {
		log.Fatal("server failed to bind")
	}

	if opts.parentPid > 0 {
		armParentWatch(opts.parentPid)
	}

	endpoint := "http://" + addr
	announceStartup(opts.readyFd, readyDetails{
		maxConcurrentRequests: opts.maxConcurrentRequests,
		maxRequestBytes:       opts.maxRequestBytes,
		endpoint:              endpoint,
		mode:                  string(mode),
		backend:               backend,
		capabilities:          capabilities,
		limits:                storeLimits,
		creds:                 creds,
		banner:                runthrough.StartupBanner(rtCfg, mode),
		region:                region,
	})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	awaitShutdown(srv, sigCh, errCh, retryCancel, retryDone)
}

func reportedRequestLimit(limit int64) int64 {
	if limit == 0 {
		return s3api.DefaultMaxRequestBytes
	}
	return limit
}

func serveRegion(value string) string {
	if region := strings.TrimSpace(value); region != "" {
		return region
	}
	return auth.DefaultRegion
}

func reportedConcurrencyLimit(limit int) int {
	if limit <= 0 {
		return s3api.DefaultMaxConcurrentRequests
	}
	return limit
}
