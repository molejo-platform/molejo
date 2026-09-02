package main

import (
	"log/slog"
	"os"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/application"
)

var version = "devel"

func main() {
	if err := application.Run(version, os.Args); err != nil {
		slog.Error("control plane stopped", "error", err)
		os.Exit(1)
	}
}
