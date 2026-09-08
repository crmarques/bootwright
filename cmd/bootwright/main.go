package main

import (
	"context"
	"io"
	"os"
	"runtime"

	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/nativeartifacts"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/trust"
	"github.com/crmarques/bootwright/internal/workspace"
)

var (
	version          string
	commit           string
	dependencyBundle string
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
		Services: cli.Services{
			Contexts:           workspace.Contexts{},
			AddOnCatalog:       addons.Catalog{},
			Secrets:            secrets.Store{},
			Encryption:         secrets.Encryption{},
			Media:              managedos.Media{},
			DesiredState:       desiredstate.Compiler{},
			Controller:         controller.Prerequisites{},
			Environment:        environment.Inspection{},
			ContainerPreflight: containercluster.Prerequisites{},
			StoragePreflight:   storage.Prerequisites{},
			AddOnPreflight:     addons.Prerequisites{},
			Lifecycle:          reconciliation.Lifecycle{},
			Artifacts:          nativeartifacts.Renderer{},
			Installer:          containercluster.Installer{},
			StorageArtifacts:   storage.Artifacts{},
			MachineInventory:   machine.Inventory{},
			MachineAccess:      machine.Access{},
			MachineTrust:       trust.Machines{},
			ClusterAccess:      containercluster.Access{},
		},
	}).Run(ctx, args)
}
