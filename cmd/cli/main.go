package main

import (
	"errors"
	"fmt"
	"os"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		// errExitQuiet means the command already printed a precise error
		// (e.g. validation failures go to stdout) — don't repeat it.
		if !errors.Is(err, errExitQuiet) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}
