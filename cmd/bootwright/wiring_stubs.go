package main

import (
	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
)

// wireStubs binds the recognized commands whose use case is unavailable. Each
// returns the shared sentinel without performing work, so the CLI renders the
// unavailable result instead of a placeholder.
func wireStubs() cli.Services {
	return cli.Services{
		AddOnCatalog:          addoncatalog.Service{},
		AddOnPreflight:        addonpreflight.Service{},
		EnvironmentPreflight:  environmentpreflight.Service{},
		EnvironmentInspection: inspection.Service{},
		EnvironmentAccess:     environmentaccess.Service{},
		ContainerPreflight:    containerpreflight.Service{},
		Installer:             installation.Service{},
		StoragePreflight:      storagepreflight.Service{},
		StorageArtifacts:      storagerendering.Service{},
		Artifacts:             artifactrendering.Service{},
	}
}
