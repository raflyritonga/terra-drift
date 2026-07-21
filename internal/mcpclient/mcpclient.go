// Package mcpclient calls terra-drift-mcp's tools via the official MCP Go SDK.
// Only the transport differs between Model A and B.
package mcpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/contract"
)

type Client struct {
	cfg     config.MCP
	version string
}

func New(cfg config.MCP, version string) *Client {
	return &Client{cfg: cfg, version: version}
}

func (c *Client) Propose(ctx context.Context, in contract.ProposalInput) (contract.ProposalOutput, error) {
	var out contract.ProposalOutput
	err := c.call(ctx, c.cfg.Tool, in, &out)
	return out, err
}

// Explain asks the server's read-only explain_drift tool for a short summary.
func (c *Client) Explain(ctx context.Context, in contract.ExplainInput) (contract.ExplainOutput, error) {
	var out contract.ExplainOutput
	err := c.call(ctx, "explain_drift", in, &out)
	return out, err
}

// call connects, invokes one tool, and decodes its structured result into out.
func (c *Client) call(ctx context.Context, tool string, args, out any) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "terra-drift", Version: c.version}, nil)
	transport, err := c.transport()
	if err != nil {
		return err
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect to MCP server: %w", err)
	}
	defer session.Close()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return fmt.Errorf("call %s: %w", tool, err)
	}
	if res.IsError {
		return fmt.Errorf("server rejected %s: %s", tool, textContent(res))
	}

	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return fmt.Errorf("re-encode structured result: %w", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse %s output: %w", tool, err)
	}
	return nil
}

// transport picks Model A (stdio subprocess) or Model B (networked) from config.
func (c *Client) transport() (mcp.Transport, error) {
	switch c.cfg.Transport {
	case "stdio", "":
		return &mcp.CommandTransport{Command: exec.Command(c.cfg.ServerBin)}, nil
	case "http":
		if c.cfg.URL == "" {
			return nil, fmt.Errorf("mcp.transport=http requires mcp.url")
		}
		return &mcp.StreamableClientTransport{Endpoint: c.cfg.URL}, nil
	default:
		return nil, fmt.Errorf("unknown mcp.transport %q", c.cfg.Transport)
	}
}

func textContent(res *mcp.CallToolResult) string {
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			return t.Text
		}
	}
	return "(no detail)"
}
