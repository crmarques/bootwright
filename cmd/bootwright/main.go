package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/cli"
)

func main() {
	ctx, stop := signalContext(context.Background())
	code := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
