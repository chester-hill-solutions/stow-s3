package mcpstorage

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (host *ObjectServer) limitCalls(next mcp.MethodHandler) mcp.MethodHandler {
	slots := make(chan struct{}, 8)
	return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
		if method != "tools/call" {
			return next(ctx, method, request)
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			if call, ok := request.(*mcp.CallToolRequest); ok && call.Params != nil {
				if len(call.Params.Arguments) > maxResponseBytes || len(call.Params.Name) > 128 {
					result, _, err := finishObject(objectResult{}, objectError{"payload_too_large"})
					return result, err
				}
			}
			result, err := next(ctx, method, request)
			return boundObjectMethodResult(result, err)
		default:
			result, _, err := finishObject(objectResult{}, objectError{"busy"})
			return result, err
		}
	}
}

func boundObjectMethodResult(result mcp.Result, err error) (mcp.Result, error) {
	if err != nil {
		return result, err
	}
	tool, ok := result.(*mcp.CallToolResult)
	if !ok {
		return result, nil
	}
	wire, encodeErr := json.Marshal(tool)
	if encodeErr == nil && len(wire) <= maxResponseBytes {
		return result, nil
	}
	value := objectResult{Outcome: "unknown"}
	structured, structuredErr := json.Marshal(tool.StructuredContent)
	if structuredErr == nil {
		_ = json.Unmarshal(structured, &value)
	}
	fallback, _, fallbackErr := minimalObjectResult(value, "response_too_large")
	return fallback, fallbackErr
}
