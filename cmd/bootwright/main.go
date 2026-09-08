package main

import (
	"context"
	"os"
)

var (
	version          string
	commit           string
	dependencyBundle string
)

func main() {
	os.Exit(runInteractive(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
