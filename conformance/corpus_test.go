package conformance_test

import (
	"testing"

	"github.com/chester-hill-solutions/stow-s3/conformance/corpus"
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestSharedCorpus(t *testing.T) {
	capabilities := sharedCorpusCapabilities(t)
	corpus.Run(t, func(t *testing.T) *corpus.Env {
		env := newTestEnv(t)
		return &corpus.Env{
			Client:   env.Client,
			Endpoint: env.Endpoint,
			Credentials: corpus.Credentials{
				AccessKeyID: env.Creds.AccessKeyID, SecretAccessKey: env.Creds.SecretAccessKey,
			},
			Status:       env.Status,
			Capabilities: capabilities,
		}
	})
}

func sharedCorpusCapabilities(t *testing.T) map[string]bool {
	t.Helper()
	store := newConformanceStore(t)
	defer store.Close()
	_, multipart := store.(storage.MultipartStore)
	return map[string]bool{"multipart": multipart, "checksum": true, "copy": true, "conditional": true}
}
