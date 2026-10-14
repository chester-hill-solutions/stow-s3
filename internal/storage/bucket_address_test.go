package storage_test

import (
	"net"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/storage"
)

func TestBucketAddressValidationPreservesIPRules(t *testing.T) {
	for _, name := range []string{"127.0.0.1", "0.0.0.0", "255.255.255.255", "001.2.3.4", "256.1.2.3", "1.2.3", "1.2.3.4.5", "123.assets", "assets_123"} {
		valid := storage.ValidateBucketName(name) == nil
		if want := net.ParseIP(name) == nil; valid != want {
			t.Errorf("bucket %q validity = %v, want %v", name, valid, want)
		}
	}
}
