// terra-drift: the client. Detects drift, walks provenance, applies edits, opens PRs.
package main

import (
	"fmt"
	"log/slog"
	"os"
)

var version = "0.5.0-dev"

const usage = `terra-drift — turn clickops drift into a reviewed pull request

Usage:
  terra-drift doctor [--dir DIR]              preflight checks
  terra-drift check  [--dir DIR] [--out FILE] [--explain]
                                              detect + tiny report; exit 0 clean / 2 drift / 1 error
  terra-drift sync   [--dir DIR] [--trust code|live|partial] [--live addr,addr]
                     [--dry-run] [--explain] [--no-pr]
                                              detect → report → ask whose version to trust → act
  terra-drift version

Exit codes: 0 = no drift, 2 = drift was found, 1 = error.
--dry-run rewrites, prints the diff, restores — no branch/commit/PR.
--explain asks the model server for a short read-only summary of the drift.
`

func main() {
	// Structured diagnostics on stderr; the human report stays on stdout.
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	var err error
	var code int
	switch os.Args[1] {
	case "doctor":
		code, err = runDoctor(os.Args[2:])
	case "check":
		code, err = runCheck(os.Args[2:])
	case "sync":
		code, err = runSync(os.Args[2:])
	case "version", "--version":
		fmt.Println("terra-drift", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
	}
	os.Exit(code)
}
