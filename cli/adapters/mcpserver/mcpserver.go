// Package mcpserver exposes every scaffold operation as an MCP tool.
package mcpserver

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/pjol/scaffold/cli/core"
)

// New builds an MCP server whose tools are kit.Operations(), named
// scaffold_<op>.
func New(kit *core.Kit, version string) (*mcp.Server, error) {
	s := mcp.NewServer(&mcp.Implementation{Name: "scaffold", Version: version}, nil)
	for _, op := range kit.Operations() {
		schema, err := op.InputSchema()
		if err != nil {
			return nil, err
		}
		op := op
		s.AddTool(&mcp.Tool{
			Name:        "scaffold_" + op.Name(),
			Description: op.Summary(),
			InputSchema: schema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			out, err := op.Invoke(ctx, req.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			text := ""
			if t, ok := out.(core.Texter); ok {
				text = t.Text()
			} else {
				b, _ := json.MarshalIndent(out, "", "  ")
				text = string(b)
			}
			return &mcp.CallToolResult{
				Content:           []mcp.Content{&mcp.TextContent{Text: text}},
				StructuredContent: out,
			}, nil
		})
	}
	return s, nil
}

// ServeStdio runs the server over stdin/stdout until the client disconnects.
func ServeStdio(ctx context.Context, kit *core.Kit, version string) error {
	s, err := New(kit, version)
	if err != nil {
		return err
	}
	return s.Run(ctx, &mcp.StdioTransport{})
}
