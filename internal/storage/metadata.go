package storage

import (
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidMetadata = errors.New("invalid user metadata")

// NormalizeUserMetadata accepts legacy prefixed keys and SDK bare keys without changing stored maps.
func NormalizeUserMetadata(metadata map[string]string) (map[string]string, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(metadata))
	for key, value := range metadata {
		name := strings.TrimPrefix(strings.ToLower(key), "x-amz-meta-")
		if !validMetadataName(name) || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%w: invalid name or value for %q", ErrInvalidMetadata, key)
		}
		if previous, exists := result[name]; exists && previous != value {
			return nil, fmt.Errorf("%w: conflicting values for %q", ErrInvalidMetadata, name)
		}
		result[name] = value
	}
	return result, nil
}

func validMetadataName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c) {
			continue
		}
		return false
	}
	return true
}
