package tool

import "github.com/mark3labs/mcp-go/mcp"

// mcpResultToResult converts an mcp-go CallToolResult to a domain Result.
func mcpResultToResult(result *mcp.CallToolResult) Result {
	if result == nil {
		return Result{}
	}
	r := Result{IsError: result.IsError}
	if len(result.Content) > 0 {
		for _, c := range result.Content {
			if text, ok := c.(mcp.TextContent); ok {
				r.Output = text.Text
				break
			}
		}
	}
	return r
}
