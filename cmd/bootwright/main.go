package main

import (
	"context"
	"os"

	"github.com/crmarques/bootwright/internal/nativeartifacts/comparisonlocal"
	"github.com/crmarques/bootwright/internal/nativeartifacts/renderlocal"
	"github.com/crmarques/bootwright/internal/workspace/invokerfs"
	"github.com/crmarques/bootwright/internal/workspace/selectionfs"
)

func main() {
	os.Exit(safeInvocation(os.Args[1:], os.Stdout, os.Stderr, processMain))
}

func processMain() int {
	if handled, code := selectionfs.ServeHelper(context.Background(), os.Args[1:], os.Stdin, os.Stdout); handled {
		return code
	}
	if handled, code := invokerfs.ServeHelper(os.Args[1:]); handled {
		return code
	}
	if handled, code := comparisonlocal.ServeHelper(context.Background(), os.Args[1:], os.Stdin, os.Stdout, renderlocal.Files{}); handled {
		return code
	}
	return runInteractive(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
}
