package mcpstorage

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func readBrowserUI(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	html, err := browserHTML()
	if err != nil {
		return nil, fmt.Errorf("browser UI unavailable")
	}
	return browserUIResult(browserURI, string(html))
}

func browserHTML() ([]byte, error) {
	if len(browserCompressed) > 512<<10 {
		return nil, fmt.Errorf("browser UI exceeds compressed bounds")
	}
	reader, err := gzip.NewReader(bytes.NewReader(browserCompressed))
	if err != nil {
		return nil, fmt.Errorf("browser UI unavailable")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("browser UI unavailable")
	}
	if len(data) > 2<<20 {
		return nil, fmt.Errorf("browser UI exceeds HTML bounds")
	}
	return data, nil
}

func browserUIResult(uri, html string) (*mcp.ReadResourceResult, error) {
	result := &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: browserMIME, Text: html, Meta: mcp.Meta{"ui": browserResourceUI{CSP: browserCSP{ConnectDomains: []string{}, ResourceDomains: []string{}}}}}}}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 4<<20 {
		return nil, fmt.Errorf("browser UI exceeds response bounds")
	}
	return result, nil
}

func workspaceBrowserHTML(overviewURI string) (string, error) {
	data, err := browserHTML()
	if err != nil {
		return "", fmt.Errorf("browser UI unavailable")
	}
	bootstrap, err := json.Marshal(workspaceBrowserLaunch{Mode: "workspace", OverviewURI: overviewURI})
	if err != nil {
		return "", fmt.Errorf("browser UI unavailable")
	}
	marker := []byte("</head>")
	index := bytes.Index(data, marker)
	if index < 0 {
		return "", fmt.Errorf("browser UI unavailable")
	}
	script := append([]byte(`<script type="application/json" id="stow-bootstrap">`), bootstrap...)
	script = append(script, []byte("</script>")...)
	output := make([]byte, 0, len(data)+len(script))
	output = append(output, data[:index]...)
	output = append(output, script...)
	output = append(output, data[index:]...)
	if len(output) > 2<<20 {
		return "", fmt.Errorf("browser UI exceeds HTML bounds")
	}
	return string(output), nil
}
