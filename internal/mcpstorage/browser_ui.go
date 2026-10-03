package mcpstorage

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func readBrowserUI(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	reader, err := gzip.NewReader(bytes.NewReader(browserCompressed))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: browserURI, MIMEType: browserMIME, Text: string(data), Meta: mcp.Meta{"ui": browserResourceUI{CSP: browserCSP{ConnectDomains: []string{}, ResourceDomains: []string{}}}}}}}, nil
}
