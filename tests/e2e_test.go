package tests

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/raflyritonga/terra-drift/internal/config"
	"github.com/raflyritonga/terra-drift/internal/contract"
	"github.com/raflyritonga/terra-drift/internal/mcpclient"
	"github.com/raflyritonga/terra-drift/internal/patch"
	"github.com/raflyritonga/terra-drift/internal/provenance"
)

// Model A: client spawns the server as a stdio subprocess.
func TestEndToEndStdio(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped in -short")
	}
	serverBin := buildServer(t)
	runPipeline(t, config.MCP{Transport: "stdio", ServerBin: serverBin, Tool: "propose_hcl_edits"})
}

// Model B: server runs as a networked HTTP service in its own process,
// the client reaches it over TCP — the different-box deployment.
func TestEndToEndHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped in -short")
	}
	serverBin := buildServer(t)
	addr := freeAddr(t)

	cmd := exec.Command(serverBin)
	cmd.Env = append(os.Environ(),
		"TERRA_DRIFT_MCP_TRANSPORT=http",
		"TERRA_DRIFT_MCP_LISTEN="+addr,
	)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	waitReachable(t, addr)

	runPipeline(t, config.MCP{Transport: "http", URL: "http://" + addr, Tool: "propose_hcl_edits"})
}

// runPipeline drives detect→walk→propose→guard→apply on the literal fixture
// and asserts the applied result matches the golden file.
func runPipeline(t *testing.T, mcpCfg config.MCP) {
	t.Helper()
	p := loadPlan(t, "drift_literal.json")
	r := p.ResourceDrift[0]
	attrs, err := r.ChangedAttrs()
	if err != nil {
		t.Fatal(err)
	}
	attr := attrs[0]

	tmp := t.TempDir()
	copyTree(t, "../testdata/hcl/literal", tmp)
	prov, err := provenance.Walk(p.Configuration, r.Address, attr.Attribute, tmp)
	if err != nil {
		t.Fatal(err)
	}

	var in contract.ProposalInput
	in.Drift.Address = r.Address
	in.Drift.Attribute = attr.Attribute
	in.Drift.Before = attr.Before
	in.Drift.After = attr.After
	in.Provenance = prov.Chain
	in.SafetyRules = []string{"never edit files under modules/**"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := mcpclient.New(mcpCfg, "test").Propose(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Edits) != 1 {
		t.Fatalf("want 1 edit, got %+v", out)
	}

	edit := out.Edits[0]
	if err := patch.Guard(edit, []string{"modules/**"}, prov); err != nil {
		t.Fatal(err)
	}
	if err := patch.Apply(tmp, edit); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(tmp, "main.tf"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../testdata/hcl/literal/golden/main.tf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("applied edit differs from golden:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func waitReachable(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(fmt.Errorf("server on %s never became reachable", addr))
}
