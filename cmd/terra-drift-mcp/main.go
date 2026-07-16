// terra-drift-mcp: the MCP server exposing propose_hcl_edits.
// The only component that ever talks to a model.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/model"
	"github.com/raflyritonga/terra-drift/internal/secret"
	"github.com/raflyritonga/terra-drift/internal/serverconfig"
	"github.com/raflyritonga/terra-drift/internal/tool"
)

var version = "0.1.0-dev"

func main() {
	configPath := flag.String("config", "", "path to terra-drift-mcp.yaml (optional)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("terra-drift-mcp", version)
		return
	}

	cfg, err := serverconfig.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	// Resolve the model key once (env or secret manager); mock needs none.
	var apiKey string
	if cfg.Model.Provider != "" && cfg.Model.Provider != "mock" {
		apiKey, err = secret.Resolve(context.Background(), cfg.Secret.Source, cfg.Secret.Ref)
		if err != nil {
			log.Fatal(err)
		}
	}
	m, err := model.New(cfg.Model.Provider, cfg.Model.ID, cfg.Model.BaseURL, apiKey)
	if err != nil {
		log.Fatal(err)
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "terra-drift-mcp", Version: version}, nil)
	h := &tool.Handler{Model: m}
	mcp.AddTool(s, &mcp.Tool{
		Name:         "propose_hcl_edits",
		Description:  "Propose structured HCL edits that reconcile a drifted attribute.",
		InputSchema:  contract.ProposalInputSchema(),
		OutputSchema: contract.ProposalOutputSchema(),
	}, h.ProposeHclEdits)

	switch cfg.Transport {
	case "stdio":
		// Model A: the client launched us as a subprocess.
		if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	case "http":
		// Model B: persistent networked service on its own box.
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
		log.Printf("terra-drift-mcp listening on %s", cfg.Listen)
		if err := http.ListenAndServe(cfg.Listen, handler); err != nil {
			log.Fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown transport %q\n", cfg.Transport)
		os.Exit(1)
	}
}
