package main

import (
	"context"
	"fmt"
	"os"

	"github.com/molejo-platform/molejo/apps/molejoctl/cmd"
)

var (
	version   = "devel"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	if err := cmd.New(version, commit, buildDate).ExecuteContext(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
