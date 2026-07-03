package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/cli"
)

func main() {
	// Cancel on SIGINT/SIGTERM so in-flight ansible process groups are reaped
	// on shutdown instead of orphaned to PID 1. os.Exit skips defers, so stop
	// the handler explicitly before exiting.
	ctx, stop := signalContext(context.Background())
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
