package runthrough_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chester-hill-solutions/stow-s3/internal/runthrough"
)

// A stub that records where a request was addressed. The point of these tests is
// the shape of the request, not its answer, so every request is refused and only
// the routing is inspected.
type addressingProbe struct {
	server *httptest.Server
	url    string
	host   string
	path   string
}

// newAddressingProbe starts a stub reachable under the name "endpoint.localhost".
//
// The name matters. The AWS SDK declines to put a bucket in the host when the
// endpoint is a bare IP address, because "127.0.0.1" is not a name a bucket can be
// prefixed onto — so a probe on 127.0.0.1 silently exercises path-style however
// the client is configured, and a virtual-hosted test against one passes for the
// wrong reason. *.localhost resolves to loopback, so the request is really made
// and the Host header is really the one the client chose.
func newAddressingProbe(t *testing.T) *addressingProbe {
	t.Helper()
	probe := &addressingProbe{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	probe.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.host = r.Host
		probe.path = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	probe.server.Listener.Close()
	probe.server.Listener = listener
	probe.server.Start()
	t.Cleanup(probe.server.Close)
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}
	probe.url = "http://endpoint.localhost:" + port
	return probe
}

func (p *addressingProbe) config(t *testing.T, addressing runthrough.Addressing) *runthrough.S3Client {
	t.Helper()
	client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{
		Endpoint:   p.url,
		AccessKey:  "AKIAEXAMPLE",
		SecretKey:  "secret",
		Region:     "us-east-1",
		Bucket:     "photos",
		Addressing: addressing,
	})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	return client
}

// The whole reason this setting exists: with path-style the bucket is in the
// path and the host is the endpoint. Asserted against the request the client
// actually sent, not against the option it was configured with, because the
// option is the thing under test.
func TestPathStylePutsTheBucketInThePath(t *testing.T) {
	probe := newAddressingProbe(t)
	client := probe.config(t, runthrough.AddressingPath)

	if _, err := client.HeadObject(context.Background(), "photos", "report.pdf"); err == nil {
		t.Fatal("expected the stub to refuse the request")
	}
	if want := "/photos/report.pdf"; probe.path != want {
		t.Fatalf("request path = %q, want %q", probe.path, want)
	}
	if probe.host != endpointHostOf(t, probe.url) {
		t.Fatalf("request host = %q, want the endpoint host with no bucket in it", probe.host)
	}
}

// Virtual-hosted moves the bucket into the host and leaves the key alone. The
// bucket must not survive in the path as well: a client that did both would
// address the same object as photos.photos.endpoint/photos/key.
func TestVirtualHostedPutsTheBucketInTheHost(t *testing.T) {
	probe := newAddressingProbe(t)
	client := probe.config(t, runthrough.AddressingVirtualHosted)

	if _, err := client.HeadObject(context.Background(), "photos", "report.pdf"); err == nil {
		t.Fatal("expected the stub to refuse the request")
	}
	endpointHost := endpointHostOf(t, probe.url)
	if want := "photos." + endpointHost; probe.host != want {
		t.Fatalf("request host = %q, want %q", probe.host, want)
	}
	if want := "/report.pdf"; probe.path != want {
		t.Fatalf("request path = %q, want %q with no bucket in it", probe.path, want)
	}
}

// A zero UpstreamConfig is the shape every existing construction site has, and it
// must still produce a path-style request. This is the assertion that fails if
// the field is ever given a zero value that is not path-style.
func TestZeroConfigStillAddressesByPath(t *testing.T) {
	probe := newAddressingProbe(t)
	client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{
		Endpoint:  probe.server.URL,
		AccessKey: "AKIAEXAMPLE",
		SecretKey: "secret",
		Region:    "us-east-1",
	})
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}
	if _, err := client.HeadObject(context.Background(), "photos", "report.pdf"); err == nil {
		t.Fatal("expected the stub to refuse the request")
	}
	if want := "/photos/report.pdf"; probe.path != want {
		t.Fatalf("request path = %q, want %q", probe.path, want)
	}
}

// Both styles must still be signed for the configured region, and the signature
// must cover the host that was actually requested. A client that rewrote the
// host after signing would produce a request the provider rejects with a
// SignatureDoesNotMatch that says nothing about addressing.
//
// This uses the named probe rather than a bare-IP stub, because against an IP
// endpoint the SDK ignores the addressing style and both arms would be testing
// path-style while claiming to test two.
func TestBothStylesAreSignedForTheConfiguredRegion(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		addressing runthrough.Addressing
	}{
		{name: "path", addressing: runthrough.AddressingPath},
		{name: "virtual-hosted", addressing: runthrough.AddressingVirtualHosted},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var authorization string
			probe := &addressingProbe{}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			probe.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authorization = r.Header.Get("Authorization")
				w.WriteHeader(http.StatusNotFound)
			}))
			probe.server.Listener.Close()
			probe.server.Listener = listener
			probe.server.Start()
			defer probe.server.Close()
			_, port, err := net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatalf("split listener address: %v", err)
			}

			client, err := runthrough.NewS3Client(runthrough.UpstreamConfig{
				Endpoint:   "http://endpoint.localhost:" + port,
				AccessKey:  "AKIAEXAMPLE",
				SecretKey:  "secret",
				Region:     "eu-west-2",
				Addressing: testCase.addressing,
			})
			if err != nil {
				t.Fatalf("NewS3Client: %v", err)
			}
			if _, err := client.HeadObject(context.Background(), "photos", "report.pdf"); err == nil {
				t.Fatal("expected the stub to refuse the request")
			}
			if authorization == "" {
				t.Fatal("request carried no Authorization header")
			}
			if region := scopeRegion(t, authorization); region != "eu-west-2" {
				t.Fatalf("signed region = %q, want eu-west-2", region)
			}
		})
	}
}

func endpointHostOf(t *testing.T, rawURL string) string {
	t.Helper()
	trimmed := rawURL
	for _, prefix := range []string{"http://", "https://"} {
		if len(trimmed) > len(prefix) && trimmed[:len(prefix)] == prefix {
			return trimmed[len(prefix):]
		}
	}
	t.Fatalf("test endpoint %q has no scheme", rawURL)
	return ""
}
