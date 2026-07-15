package main

import "os"

// isInteractive reports whether stdin is a terminal (so we can prompt).
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
