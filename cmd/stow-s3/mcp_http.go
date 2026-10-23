package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The MCP surface over the MCP streamable HTTP transport, sessionless: these tools
// are request/response over one configured workspace or bucket, and a session table
// would be state this surface has no use for. Exposure follows the S3 listener's
// posture (ADR 0021), and the token is a caller credential rather than an
// operations one, so it is separate from the admin token.
const (
	mcpHTTPReadHeaderTimeout = 10 * time.Second
	mcpHTTPIdleTimeout       = 2 * time.Minute
	mcpHTTPMaxHeaderBytes    = 1 << 20
	mcpHTTPShutdownTimeout   = 5 * time.Second
)

// validateMCPExposure refuses an MCP HTTP address that would be reachable beyond
// loopback without the consent and the token that decision requires.
func validateMCPExposure(addr, token string, allowInsecureRemote bool) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid --http-addr %q: want host:port", addr)
	}
	if listenHostIsLoopback(host) {
		return nil
	}
	if !allowInsecureRemote {
		return fmt.Errorf("refusing to serve MCP on %q: it is not a loopback address; terminate TLS in front of this process and pass --allow-insecure-remote to accept that, or use --http-addr 127.0.0.1:PORT", addr)
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("serving MCP on %q requires --mcp-token (or STOW_MCP_TOKEN): without one every caller that is not loopback is refused, so nothing could use it", addr)
	}
	return nil
}

// bearerToken reads a bearer credential from an Authorization header, and returns
// "" for anything else. A scheme other than Bearer is not a credential.
func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// mcpHTTPHandler wraps the transport handler with the token check. An empty token
// means the caller accepted a loopback-only address, where the process boundary is
// the protection and the S3 listener next to it needs no token either.
func mcpHTTPHandler(server *mcp.Server, token string) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	if token == "" {
		return handler
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := bearerToken(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="stow-mcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// runMCPHTTP serves the surface until the context is cancelled. The address is
// printed once it is bound, so a launcher that asked for port 0 learns the real
// port from the same channel the S3 listener uses.
func runMCPHTTP(ctx context.Context, server *mcp.Server, addr, token string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Handler:           mcpHTTPHandler(server, token),
		ReadHeaderTimeout: mcpHTTPReadHeaderTimeout,
		IdleTimeout:       mcpHTTPIdleTimeout,
		MaxHeaderBytes:    mcpHTTPMaxHeaderBytes,
	}
	fmt.Printf("mcp http listening on http://%s\n", listener.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.Serve(listener) }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), mcpHTTPShutdownTimeout)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
		return nil
	}
}
