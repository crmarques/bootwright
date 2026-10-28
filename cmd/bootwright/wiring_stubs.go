package main

import (
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
)

// wireStubs binds the recognized commands whose use case is unavailable. Each
// returns the shared sentinel without performing work, so the CLI renders the
// unavailable result instead of a placeholder.
func wireStubs() cli.Services {
	return cli.Services{
		AddOnPreflight:   addonpreflight.Service{},
		Installer:        installation.Service{},
		StorageArtifacts: storagerendering.Service{},
	}
}
