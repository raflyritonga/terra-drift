package tests

import (
	"os"
	"strings"
	"testing"
)

// Only the official MCP SDK is allowed in go.mod.
func TestOnlyOfficialMCPSDK(t *testing.T) {
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	gomod := string(data)
	for _, forbidden := range []string{"mark3labs/mcp-go", "metoro-io/mcp-golang"} {
		if strings.Contains(gomod, forbidden) {
			t.Fatalf("go.mod pulls in forbidden MCP SDK %q", forbidden)
		}
	}
	if !strings.Contains(gomod, "github.com/modelcontextprotocol/go-sdk") {
		t.Fatal("go.mod is missing the official MCP SDK")
	}
}
