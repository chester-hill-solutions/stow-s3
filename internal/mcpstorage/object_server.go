package mcpstorage

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"github.com/chester-hill-solutions/stow-s3/pkg/stow"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxObjectPayload = 64 << 10
const maxObjectObservations = 256
const observationTTL = 15 * time.Minute

type ObjectConfig struct {
	Dir, Bucket                     string
	MaxBytes, MaxObjects            int64
	MaxObjectBytes, MaxObservations int
	CreateBucket, ReadOnly          bool
	Browser                         bool
}

type objectObservation struct {
	key       string
	condition stow.SaveCondition
	expires   time.Time
}

type ObjectServer struct {
	Server       *mcp.Server
	runtime      *stow.Runtime
	config       ObjectConfig
	mu           sync.Mutex
	observations map[string]objectObservation
	now          func() time.Time
	closed       bool
}

func NewObjects(config ObjectConfig) (*ObjectServer, error) {
	if err := normalizeObjectConfig(&config); err != nil {
		return nil, err
	}
	authority := stow.ReadWrite()
	if config.ReadOnly {
		authority = stow.ReadOnly()
	}
	runtime, err := stow.OpenFilesystem(stow.FilesystemOptions{Dir: config.Dir, Options: stow.Options{Authority: &authority, MaxBytes: config.MaxBytes, MaxObjects: config.MaxObjects}})
	if err != nil {
		return nil, fmt.Errorf("open configured object storage: %w", err)
	}
	host := &ObjectServer{runtime: runtime, config: config, observations: make(map[string]objectObservation), now: time.Now}
	if err := host.requireBucket(); err != nil {
		_ = runtime.Close()
		return nil, err
	}
	host.Server = mcp.NewServer(&mcp.Implementation{Name: "stow-objects", Version: "1"}, &mcp.ServerOptions{Instructions: objectGuide})
	host.addTools()
	if config.Browser {
		host.addBrowser()
	}
	return host, nil
}

func normalizeObjectConfig(config *ObjectConfig) error {
	if config.Dir == "" || storage.ValidateBucketName(config.Bucket) != nil {
		return fmt.Errorf("explicit object directory and valid bucket required")
	}
	if config.ReadOnly && config.CreateBucket {
		return fmt.Errorf("read-only object host cannot create a bucket")
	}
	if config.MaxBytes < 0 || config.MaxObjects < 0 || config.MaxObjectBytes < 0 || config.MaxObservations < 0 {
		return fmt.Errorf("object bounds must not be negative")
	}
	if config.MaxObjectBytes > maxObjectPayload || config.MaxObservations > maxObjectObservations {
		return fmt.Errorf("object bounds exceed adapter limits")
	}
	if config.MaxObjectBytes == 0 {
		config.MaxObjectBytes = maxObjectPayload
	}
	if config.MaxObservations == 0 {
		config.MaxObservations = maxObjectObservations
	}
	dir, err := filepath.Abs(config.Dir)
	if err != nil {
		return err
	}
	config.Dir = dir
	return nil
}

func (host *ObjectServer) requireBucket() error {
	buckets, err := host.runtime.ListBuckets(context.Background())
	if err != nil {
		return err
	}
	for _, bucket := range buckets {
		if bucket.Name == host.config.Bucket {
			return nil
		}
	}
	if !host.config.CreateBucket {
		return fmt.Errorf("configured object bucket does not exist")
	}
	return host.runtime.CreateBucket(context.Background(), host.config.Bucket)
}

func (host *ObjectServer) Close() error {
	host.mu.Lock()
	defer host.mu.Unlock()
	host.closed = true
	clear(host.observations)
	return host.runtime.Close()
}

const objectGuide = "This host exposes one configured bucket. Read before editing and use its live observation token with a persisted random request_key. Use replace:true only for deliberate replacement. Preserve the same token, request key, bytes and options on live retries. After reopening, resolve the original key instead of reading again and blindly retrying. Only outcome=committed proves a save; not_found does not prove no prior effect. Observation tokens expire after 15 minutes, are bounded and can be released. These tools do not coordinate arbitrary host file edits, grant resource ACLs, or retain historical object bodies."
