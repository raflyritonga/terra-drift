// terra-drift-mcp: the MCP server exposing propose_hcl_edits and explain_drift.
// The only component that ever talks to a model. HCL in → edits out; it has
// no terraform, AWS, git, or repo access, and its only egress is the model.
package main

import (
	"context"
	"crypto/subtle"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/model"
	"github.com/raflyritonga/terra-drift/internal/secret"
	"github.com/raflyritonga/terra-drift/internal/serverconfig"
	"github.com/raflyritonga/terra-drift/internal/tool"
)

var version = "0.5.0-dev"

func main() {
	configPath := flag.String("config", "", "path to terra-drift-mcp.yaml (optional)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("terra-drift-mcp", version, "contract", contract.Version)
		return
	}

	// Structured journald-friendly logs; nothing sensitive is ever logged.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

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

	h := tool.NewHandler(m, cfg)
	s := mcp.NewServer(&mcp.Implementation{Name: "terra-drift-mcp", Version: version}, nil)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "propose_hcl_edits",
		Description:  "Propose structured HCL edits that reconcile a drifted attribute.",
		InputSchema:  contract.ProposalInputSchema(),
		OutputSchema: contract.ProposalOutputSchema(),
	}, h.ProposeHclEdits)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "explain_drift",
		Description:  "Explain drifted attributes and the risk of reverting them. Read-only.",
		InputSchema:  contract.ExplainInputSchema(),
		OutputSchema: contract.ExplainOutputSchema(),
	}, h.ExplainDrift)

	switch cfg.Transport {
	case "stdio":
		// Model A: the client launched us as a subprocess.
		if err := s.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	case "http":
		// Model B: persistent networked service. Network isolation is not
		// authorization: the MCP endpoint requires a bearer token.
		if cfg.AuthToken == "" {
			log.Fatal("http transport requires TERRA_DRIFT_MCP_AUTH_TOKEN (fail closed)")
		}
		mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
		mux := http.NewServeMux()
		mux.Handle("/", requireBearer(cfg.AuthToken, mcpHandler))
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok\n") })
		mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ready\n") })
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, h.Metrics.Render()) })
		slog.Info("listening", "addr", cfg.Listen, "contract", contract.Version, "provider", cfg.Model.Provider)
		if err := http.ListenAndServe(cfg.Listen, mux); err != nil {
			log.Fatal(err)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown transport %q\n", cfg.Transport)
		os.Exit(1)
	}
}

// requireBearer guards the MCP endpoint with a constant-time token check.
func requireBearer(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		want := "Bearer " + token
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "unauthorized\n")
			return
		}
		next.ServeHTTP(w, r)
	})
}
