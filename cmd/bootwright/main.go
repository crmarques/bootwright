package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/workspace/selectionfs"
)

var (
	version          string
	commit           string
	dependencyBundle string
)

func main() {
	if handled, code := selectionfs.ServeHelper(context.Background(), os.Args[1:], os.Stdin, os.Stdout); handled {
		os.Exit(code)
	}
	os.Exit(runInteractive(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
