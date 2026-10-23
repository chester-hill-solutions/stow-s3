package main

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpHTTPTestServer starts the real binary in object mode over streamable HTTP and
// returns the endpoint it reported. The claim under test is about a process: the
// address it prints is the one that answers, and port 0 is a request rather than a
// promise.
func mcpHTTPTestServer(t *testing.T, extra ...string) string {
	t.Helper()
	binary := launcherBinary(t)
	args := append([]string{
		"mcp",
		"--object-dir", t.TempDir(),
		"--bucket", "probe",
		"--create-bucket",
		"--http-addr", "127.0.0.1:0",
	}, extra...)
	cmd := exec.Command(binary, args...)
	cmd.Env = envWithout("STOW_MCP_TOKEN")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("start mcp http server: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	endpoint := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "mcp http listening on ") {
				endpoint <- strings.TrimPrefix(line, "mcp http listening on ")
				return
			}
		}
		close(endpoint)
	}()

	select {
	case reported, ok := <-endpoint:
		if !ok {
			t.Fatal("the server exited before it reported an endpoint")
		}
		if !strings.HasPrefix(reported, "http://127.0.0.1:") {
			t.Fatalf("endpoint = %q, want a loopback address", reported)
		}
		return reported
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not report an endpoint")
		return ""
	}
}

// The transport has to be the MCP streamable HTTP transport, so the client under
// test is the SDK's own client rather than a hand-rolled request sequence.
func TestMCPHTTPServesTheToolsOverStreamableHTTP(t *testing.T) {
	endpoint := mcpHTTPTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	names := make(map[string]bool, len(tools.Tools))
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"stow_object_capabilities", "stow_object_save", "stow_object_read_for_save"} {
		if !names[want] {
			t.Errorf("the HTTP surface is missing %q; it has %v", want, names)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "stow_object_capabilities"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.IsError {
		t.Fatalf("stow_object_capabilities reported an error: %+v", result.Content)
	}
}

// A token makes the transport a credentialed surface, and the refusal has to be a
// refusal rather than a silently empty session.
func TestMCPHTTPRequiresTheTokenWhenOneIsSet(t *testing.T) {
	endpoint := mcpHTTPTestServer(t, "--mcp-token", "s3cret")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	unauthenticated := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "1"}, nil)
	if session, err := unauthenticated.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil); err == nil {
		_ = session.Close()
		t.Fatal("a caller with no token connected")
	}

	authenticated := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: "s3cret"}},
	}
	session, err := authenticated.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect with the token: %v", err)
	}
	defer func() { _ = session.Close() }()
	if _, err := session.ListTools(ctx, nil); err != nil {
		t.Fatalf("list tools with the token: %v", err)
	}
}

type bearerRoundTripper struct{ token string }

func (t bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
}

// Exposure is the same decision as the S3 listener, and the refusal happens before
// a socket is opened.
func TestMCPHTTPRefusesAnAddressBeyondLoopback(t *testing.T) {
	binary := launcherBinary(t)
	cases := []struct {
		name        string
		args        []string
		wantMessage string
	}{
		{
			name:        "a wildcard address needs consent",
			args:        []string{"--http-addr", "0.0.0.0:0"},
			wantMessage: "--allow-insecure-remote",
		},
		{
			name:        "consent without a token is refused",
			args:        []string{"--http-addr", "192.0.2.1:0", "--allow-insecure-remote"},
			wantMessage: "--mcp-token",
		},
		{
			name:        "a malformed address is refused",
			args:        []string{"--http-addr", "not-an-address"},
			wantMessage: "invalid --http-addr",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"mcp", "--object-dir", t.TempDir(), "--bucket", "probe", "--create-bucket"}, testCase.args...)
			cmd := exec.Command(binary, args...)
			cmd.Env = envWithout("STOW_MCP_TOKEN")
			output, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the server started on the refused address: %s", output)
			}
			if !strings.Contains(string(output), testCase.wantMessage) {
				t.Errorf("refusal = %q, want it to name %s", output, testCase.wantMessage)
			}
			if strings.Contains(string(output), "mcp http listening") {
				t.Errorf("the server reported a listener after refusing: %s", output)
			}
		})
	}
}
