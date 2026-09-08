package main

import (
	"context"
	"io"
	"runtime"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runServices(ctx, args, stdout, stderr, wireServices())
}

func runInteractive(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	confirmer := cli.NewConfirmation(readStdin, stderr, stdinTerminal)
	return runServices(ctx, args, stdout, stderr, wireContextServices(contextfs.New(contextfs.Options{}), confirmer), beginSignalOperation)
}

func runServices(ctx context.Context, args []string, stdout, stderr io.Writer, services cli.Services, operations ...func(context.Context) (context.Context, func())) int {
	var operation func(context.Context) (context.Context, func())
	if len(operations) > 0 {
		operation = operations[0]
	}
	return cli.New(cli.Config{
		Out:    stdout,
		ErrOut: stderr,
		BuildInfo: cli.BuildInfo{
			Version:          version,
			Commit:           commit,
			GoVersion:        runtime.Version(),
			GOOS:             runtime.GOOS,
			GOARCH:           runtime.GOARCH,
			DependencyBundle: dependencyBundle,
		},
		Services:            services,
		BeginOperation:      operation,
		EncodeEffectiveYAML: encoding.YAML,
		EncodeEffectiveJSON: encoding.JSON,
	}).Run(ctx, args)
}
