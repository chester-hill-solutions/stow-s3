package mcpstorage

import "github.com/modelcontextprotocol/go-sdk/mcp"

func closedWorldHint() *bool {
	value := false
	return &value
}

func readOnlyTool(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: closedWorldHint()}
}

func writeTool(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &destructive, OpenWorldHint: closedWorldHint()}
}
