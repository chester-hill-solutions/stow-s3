package main

import (
	"flag"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/auth"
)

type serveFlags struct {
	port             int
	dataDir          string
	backendFlag      string
	accessKey        string
	secretKey        string
	host             string
	baseHost         string
	allowPublicAdmin bool
	adminToken       string
	modeFlag         string
	regionFlag       string
	allowLiveWrites  bool
	readOnly         bool
	cacheDir         string
	cacheMaxBytes    int64
	cacheMaxObjects  int64
	cacheTTL         time.Duration
	offline          *offlineFlag
	showVersion      bool
	maxBytes         int64
	maxObjects       int64
	readyFd          int
	parentPid        int
	corsOrigins      corsOriginList
}

// parseServeFlags declares every flag serve accepts and returns what was asked for.
//
// It is a function rather than thirty declarations inside serve because a reader who
// opens serve should see what serve *does*, and a wall of flag pointers in front of it
// is the one thing that stops them. The flags are also the part most likely to change —
// a cache limit, a readiness spelling — and a change to one should not require reading
// the wiring behind it.
//
// ExitOnError, so a flag the package cannot read has already printed why and exited.
func parseServeFlags(args []string) serveFlags {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	port := flags.Int("port", 9000, "HTTP listen port (0 = ephemeral)")
	dataDir := flags.String("data-dir", ".stow", "Data directory for object storage")
	backendFlag := flags.String("backend", "filesystem", "Storage backend (filesystem or memory)")
	accessKey := flags.String("access-key", "", "Access key (generated if omitted)")
	secretKey := flags.String("secret-key", "", "Secret key (generated if omitted)")
	host := flags.String("host", "127.0.0.1", "Listen host")
	baseHost := flags.String("base-host", "", "Host suffix for virtual-hosted-style routing")
	allowPublicAdmin := flags.Bool("allow-public-admin", false, "Deprecated and ignored: remote admin routes now require --admin-token")
	adminToken := flags.String("admin-token", "", "Credential for admin and metrics routes; prefer STOW_ADMIN_TOKEN so it is not visible in the process list")
	var corsOrigins corsOriginList
	flags.Var(&corsOrigins, "cors-origin", "Browser origin permitted to read responses; repeatable. Default permits loopback origins only")
	modeFlag := flags.String("mode", "", "Operational mode: local or run-through. Run-through is never selected implicitly; ask for it with --mode run-through or STOW_MODE=run-through")
	regionFlag := flags.String("region", auth.DefaultRegion, "Region the server verifies signatures against and reports in the readiness message")
	allowLiveWrites := flags.Bool("allow-live-writes", false, "Propagate writes to upstream S3")
	readOnly := flags.Bool("read-only", false, "Serve reads and lists only: writes, deletes, bucket changes and upstream access are refused")
	cacheDir := flags.String("cache-dir", "", "Run-through cache directory (default: <data-dir>/cache)")
	cacheMaxBytes := flags.Int64("cache-max-bytes", -1, "Maximum separate cache bytes (0 disables the limit; -1 uses environment)")
	cacheMaxObjects := flags.Int64("cache-max-objects", -1, "Maximum separate cache objects (0 disables the limit; -1 uses environment)")
	cacheTTL := flags.Duration("cache-ttl", -1, "Separate cache entry lifetime (0 disables expiry; -1 uses environment)")
	// A bool, and it stays a bool. The obvious way to get an explicit false is a
	// string flag, and that is a trap: `--offline` as a string consumed the next
	// argument, so `--offline --ready-fd 3` failed with "invalid --offline
	offline := registerOfflineFlag(flags)
	showVersion := versionFlag(flags)
	maxBytes := flags.Int64("max-bytes", 0, "Maximum stored object bytes enforced on every native S3 request (0 disables the limit)")
	maxObjects := flags.Int64("max-objects", 0, "Maximum stored object count enforced on every native S3 request (0 disables the limit)")
	readyFd := flags.Int("ready-fd", -1, "Write one machine-readable readiness object to this file descriptor instead of the STOW_READY line on stdout")
	parentPid := flags.Int("parent-pid", 0, "Exit when this parent process dies (0 disables the watch, which is the default for a hand-run server)")
	// The error is safe to discard: this flag set is ExitOnError, so a flag the
	// package cannot read has already printed why and exited. See offline.go.
	flags.Parse(args)
	return serveFlags{
		port:             *port,
		dataDir:          *dataDir,
		backendFlag:      *backendFlag,
		accessKey:        *accessKey,
		secretKey:        *secretKey,
		host:             *host,
		baseHost:         *baseHost,
		allowPublicAdmin: *allowPublicAdmin,
		adminToken:       *adminToken,
		modeFlag:         *modeFlag,
		regionFlag:       *regionFlag,
		allowLiveWrites:  *allowLiveWrites,
		readOnly:         *readOnly,
		cacheDir:         *cacheDir,
		cacheMaxBytes:    *cacheMaxBytes,
		cacheMaxObjects:  *cacheMaxObjects,
		cacheTTL:         *cacheTTL,
		offline:          offline,
		showVersion:      *showVersion,
		maxBytes:         *maxBytes,
		maxObjects:       *maxObjects,
		readyFd:          *readyFd,
		parentPid:        *parentPid,
		corsOrigins:      corsOrigins,
	}
}
