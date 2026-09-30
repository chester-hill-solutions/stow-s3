package runthrough

import "strings"

// Addressing is how a request names its bucket: in the path, or in the host.
//
// Path-style stays the default. It is the style every S3-compatible endpoint
// accepts, the style the local server and the AWS SDKs use by default, and the
// one that works with a bucket name containing characters a hostname cannot
// carry. Virtual-hosted is a request, and like every other request in this
// package it has to be made by name rather than inferred.
type Addressing string

const (
	// AddressingPath puts the bucket in the request path:
	// https://endpoint/bucket/key.
	AddressingPath Addressing = "path"
	// AddressingVirtualHosted puts the bucket in the host:
	// https://bucket.endpoint/key.
	AddressingVirtualHosted Addressing = "virtual-hosted"
)

// ParseAddressing resolves an addressing style, reporting false for a value it
// does not recognise.
//
// An empty or blank value is path-style rather than an error: an absent variable
// is not a request for anything, and the zero UpstreamConfig has to mean the
// behavior every existing construction site already had.
//
// Every other unrecognized value is refused rather than defaulted. A misspelling
// that quietly became path-style would send every request to a host the provider
// does not serve, which is the same failure ADR 0011 removed from mode selection:
// the operator is told the configuration was accepted and finds out from a DNS
// failure instead.
func ParseAddressing(raw string) (Addressing, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "path":
		return AddressingPath, true
	case "virtual-hosted", "virtualhosted", "vhost":
		return AddressingVirtualHosted, true
	default:
		return "", false
	}
}

// addressing returns the resolved style, treating an unparseable value as
// path-style. It is for the request-building path, where configuration has
// already been validated by ConfigFromEnvChecked and a bad value must not be
// able to stop a request being made at all.
func (c UpstreamConfig) addressing() Addressing {
	if parsed, ok := ParseAddressing(string(c.Addressing)); ok {
		return parsed
	}
	return AddressingPath
}

// UseVirtualHosted reports whether requests should carry the bucket in the host.
func (c UpstreamConfig) UseVirtualHosted() bool {
	return c.addressing() == AddressingVirtualHosted
}
