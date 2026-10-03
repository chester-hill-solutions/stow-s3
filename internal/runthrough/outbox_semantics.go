package runthrough

import (
	"github.com/chester-hill-solutions/stow-s3/internal/storage"
	"maps"
)

func samePropagatedObject(remote, local *storage.ObjectMeta) bool {
	if !storage.ETagEqual(remote.ETag, local.ETag) || effectiveContentType(remote.ContentType) != effectiveContentType(local.ContentType) {
		return false
	}
	wanted, err := storage.NormalizeUserMetadata(local.Metadata)
	if err != nil {
		return false
	}
	actual, err := storage.NormalizeUserMetadata(remote.Metadata)
	if err != nil || !maps.Equal(actual, wanted) {
		return false
	}
	return local.ChecksumValue == "" || remote.ChecksumAlgorithm == local.ChecksumAlgorithm && remote.ChecksumValue == local.ChecksumValue
}

func effectiveContentType(value string) string {
	if value == "" {
		return "application/octet-stream"
	}
	return value
}
