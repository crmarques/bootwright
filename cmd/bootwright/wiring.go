package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/addons"
	addoncatalog "github.com/crmarques/bootwright/internal/addons/catalog"
	addonpreflight "github.com/crmarques/bootwright/internal/addons/preflight"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster"
	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/containercluster/installation"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/environment"
	environmentaccess "github.com/crmarques/bootwright/internal/environment/access"
	"github.com/crmarques/bootwright/internal/environment/inspection"
	environmentpreflight "github.com/crmarques/bootwright/internal/environment/preflight"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/managedos/media"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/localstore"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
	secretstorage "github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/storage"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
	storagerendering "github.com/crmarques/bootwright/internal/storage/rendering"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/trust/enrollment"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func wireCompiler() compilation.Compiler {
	selection := compilation.NewGraphSelector(addons.StorageAttachments, environment.Select)
	return compilation.NewCompiler(yamlstream.Parser{}, selection.Select, compilation.Rules{Normalize: environment.Normalize, ValidateAuthored: environment.ValidateAuthored, ValidatePartial: environment.ValidatePartial, Validate: environment.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidateAuthored, Validate: secrets.Validate},
		compilation.Rules{Normalize: addons.Normalize, ValidateAuthored: addons.ValidateAuthored, ValidatePartial: addons.ValidatePartial, Validate: addons.Validate},
		compilation.Rules{Normalize: customplaybooks.Normalize, ValidateAuthored: customplaybooks.ValidateAuthored, ValidatePartial: customplaybooks.ValidatePartial, Validate: customplaybooks.Validate},
		compilation.Rules{Normalize: storage.Normalize, ValidateAuthored: storage.ValidateAuthored, ValidatePartial: storage.ValidatePartial, Validate: storage.Validate},
		compilation.Rules{Normalize: machine.Normalize, ValidateAuthored: machine.ValidateAuthored, ValidatePartial: machine.ValidatePartial, Validate: machine.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, ValidatePartial: substrate.ValidatePartial, Validate: substrate.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, ValidatePartial: managedos.ValidatePartial, Validate: managedos.Validate},
		compilation.Rules{Normalize: containercluster.Normalize, ValidateAuthored: containercluster.ValidateAuthored, ValidatePartial: containercluster.ValidatePartial, Validate: containercluster.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, ValidatePartial: infrastructureservices.ValidatePartial, Validate: infrastructureservices.Validate})
}

func wireServices() cli.Services { return wireLocalServices(nil, nil) }

func wireLocalServices(confirmer contexts.Confirmer, input secretmaterial.InputReader) cli.Services {
	repository := contextfs.New(contextfs.Options{})
	return wireContextServices(repository, repository, confirmer, input)
}

func wireContextServices(repository contexts.Repository, workspace secretstorage.Workspace, confirmer contexts.Confirmer, inputs ...secretmaterial.InputReader) cli.Services {
	compiler := wireCompiler()
	access := secretstorage.NewAccess(workspace, secretstorage.NewCatalog(localstore.New()), nil)
	var input secretmaterial.InputReader
	if len(inputs) != 0 {
		input = inputs[0]
	}
	return cli.Services{
		Contexts:              contexts.New(inputfs.Reader{}, compiler, repository, contextguard.Guard{}, confirmer),
		AddOnCatalog:          addoncatalog.Service{},
		Secrets:               custody.New(access, compiler, secretmaterial.New(input), confirmer),
		Encryption:            encryption.New(access, confirmer),
		Media:                 media.Service{},
		DesiredState:          compilation.New(inputfs.Reader{}, compiler, contexts.Inputs{Repository: repository}),
		Controller:            prerequisites.Service{},
		EnvironmentPreflight:  environmentpreflight.Service{},
		EnvironmentInspection: inspection.Service{},
		EnvironmentAccess:     environmentaccess.Service{},
		ContainerPreflight:    containerpreflight.Service{},
		StoragePreflight:      storagepreflight.Service{},
		AddOnPreflight:        addonpreflight.Service{},
		Lifecycle:             lifecycle.Service{},
		Artifacts:             artifactrendering.Service{},
		Installer:             installation.Service{},
		StorageArtifacts:      storagerendering.Service{},
		MachineInventory:      inventory.Service{},
		MachineAccess:         machineaccess.Service{},
		MachineTrust:          enrollment.Service{},
		ClusterAccess:         containeraccess.Service{},
	}
}

type secretInputFunc func(context.Context, []byte) (int, error)

func (f secretInputFunc) Read(ctx context.Context, buffer []byte) (int, error) { return f(ctx, buffer) }
