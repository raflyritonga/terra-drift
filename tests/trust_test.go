package tests

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/raflyritonga/terra-drift/internal/drift"
	"github.com/raflyritonga/terra-drift/internal/trust"
)

var twoItems = []drift.ResourceReport{
	{Address: "aws_security_group.web", Attrs: []string{"cidr_blocks"}},
	{Address: "module.net.aws_route.r", Attrs: []string{"destination"}},
}

func resolve(t *testing.T, mode, csv, stdin string, interactive bool) (map[string]bool, bool, error) {
	t.Helper()
	return trust.Resolve(mode, csv, twoItems, interactive,
		bufio.NewReader(strings.NewReader(stdin)), io.Discard)
}

func TestTrustCodeChangesNothing(t *testing.T) {
	live, decided, err := resolve(t, "code", "", "", false)
	if err != nil || !decided || len(live) != 0 {
		t.Fatalf("code: live=%v decided=%v err=%v", live, decided, err)
	}
}

func TestTrustLiveSelectsAll(t *testing.T) {
	live, decided, err := resolve(t, "live", "", "", false)
	if err != nil || !decided || len(live) != 2 {
		t.Fatalf("live: live=%v decided=%v err=%v", live, decided, err)
	}
}

func TestTrustPartialCSV(t *testing.T) {
	live, decided, err := resolve(t, "partial", "aws_security_group.web", "", false)
	if err != nil || !decided {
		t.Fatalf("partial csv: decided=%v err=%v", decided, err)
	}
	if !live["aws_security_group.web"] || live["module.net.aws_route.r"] {
		t.Fatalf("partial csv selected wrong set: %v", live)
	}
}

func TestTrustPartialNonInteractiveNeedsList(t *testing.T) {
	if _, _, err := resolve(t, "partial", "", "", false); err == nil {
		t.Fatal("expected error: partial without --live in non-interactive mode")
	}
}

func TestTrustUndecidedInCI(t *testing.T) {
	_, decided, err := resolve(t, "", "", "", false)
	if err != nil || decided {
		t.Fatalf("unset non-interactive should be undecided: decided=%v err=%v", decided, err)
	}
}

func TestTrustBadValue(t *testing.T) {
	if _, _, err := resolve(t, "sideways", "", "", false); err == nil {
		t.Fatal("expected error for unknown trust mode")
	}
}

func TestTrustInteractivePromptLive(t *testing.T) {
	live, decided, err := resolve(t, "", "", "live\n", true)
	if err != nil || !decided || len(live) != 2 {
		t.Fatalf("interactive live: live=%v decided=%v err=%v", live, decided, err)
	}
}

func TestTrustInteractiveQuit(t *testing.T) {
	_, decided, err := resolve(t, "", "", "quit\n", true)
	if err != nil || decided {
		t.Fatalf("interactive quit should be undecided: decided=%v err=%v", decided, err)
	}
}

func TestTrustInteractivePartialPerResource(t *testing.T) {
	// answer "code" for the first resource, "live" for the second
	live, decided, err := resolve(t, "partial", "", "code\nlive\n", true)
	if err != nil || !decided {
		t.Fatalf("interactive partial: decided=%v err=%v", decided, err)
	}
	if live["aws_security_group.web"] || !live["module.net.aws_route.r"] {
		t.Fatalf("per-resource selection wrong: %v", live)
	}
}
