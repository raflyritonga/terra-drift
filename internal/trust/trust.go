// Package trust resolves whose version to trust for drifted resources:
// the code (change nothing) or live infrastructure (rewrite the code).
package trust

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/raflyritonga/terra-drift/internal/drift"
)

// Resolve returns the set of resource addresses to rewrite code for.
// An empty set with decided=true means "trust code" — change nothing.
// decided=false means no choice was made and the caller should stop.
func Resolve(mode, liveCSV string, items []drift.ResourceReport, interactive bool, in *bufio.Reader, out io.Writer) (map[string]bool, bool, error) {
	switch mode {
	case "code":
		return map[string]bool{}, true, nil
	case "live":
		return allAddrs(items), true, nil
	case "partial":
		if liveCSV != "" {
			return csvSet(liveCSV), true, nil
		}
		if interactive {
			return promptPerResource(items, in, out), true, nil
		}
		return nil, false, fmt.Errorf(`--trust partial needs --live "addr,addr" when not interactive`)
	case "":
		if !interactive {
			return nil, false, nil
		}
		return promptOverall(items, in, out)
	default:
		return nil, false, fmt.Errorf("--trust must be code, live, or partial (got %q)", mode)
	}
}

func promptOverall(items []drift.ResourceReport, in *bufio.Reader, out io.Writer) (map[string]bool, bool, error) {
	fmt.Fprint(out, "\ntrust which version? [code/live/partial/quit]: ")
	switch readWord(in) {
	case "code", "c":
		return map[string]bool{}, true, nil
	case "live", "l":
		return allAddrs(items), true, nil
	case "partial", "p":
		return promptPerResource(items, in, out), true, nil
	default:
		return nil, false, nil
	}
}

func promptPerResource(items []drift.ResourceReport, in *bufio.Reader, out io.Writer) map[string]bool {
	live := map[string]bool{}
	for _, it := range items {
		fmt.Fprintf(out, "  %s — trust [code/live/skip]: ", it.Address)
		if w := readWord(in); w == "live" || w == "l" {
			live[it.Address] = true
		}
	}
	return live
}

func readWord(in *bufio.Reader) string {
	line, _ := in.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line))
}

func allAddrs(items []drift.ResourceReport) map[string]bool {
	m := map[string]bool{}
	for _, it := range items {
		m[it.Address] = true
	}
	return m
}

func csvSet(s string) map[string]bool {
	m := map[string]bool{}
	for a := range strings.SplitSeq(s, ",") {
		if a = strings.TrimSpace(a); a != "" {
			m[a] = true
		}
	}
	return m
}
