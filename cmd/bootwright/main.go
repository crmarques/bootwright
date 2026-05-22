package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
