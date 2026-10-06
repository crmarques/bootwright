package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/containercluster"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/customplaybooks"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// wireCompiler binds the syntax adapter, the graph selection functions and each
// context's pure admission rules. Composition supplies the functions; the
// compiler owns phase order and diagnostics.
func wireCompiler() compilation.Compiler {
	selection := compilation.NewGraphSelector(addons.StorageAttachments, environment.Select)
	return compilation.NewCompiler(yamlstream.Parser{}, selection.Select, compilation.Rules{Normalize: environment.Normalize, Validate: environment.Validate},
		compilation.Rules{Normalize: secrets.Normalize, ValidateAuthored: secrets.ValidateAuthored, ValidatePartial: secrets.ValidatePartial, Validate: secrets.Validate},
		compilation.Rules{Normalize: addons.Normalize, ValidateAuthored: addons.ValidateAuthored, ValidatePartial: addons.ValidatePartial, Validate: addons.Validate},
		compilation.Rules{Normalize: customplaybooks.Normalize, ValidateAuthored: customplaybooks.ValidateAuthored, ValidatePartial: customplaybooks.ValidatePartial, Validate: customplaybooks.Validate},
		compilation.Rules{Normalize: storage.Normalize, ValidateAuthored: storage.ValidateAuthored, ValidatePartial: storage.ValidatePartial, Validate: storage.Validate},
		compilation.Rules{Normalize: machine.Normalize, NormalizationOrigins: machine.NormalizationOrigins, ValidateAuthored: machine.ValidateAuthored, ValidatePartial: machine.ValidatePartial, Validate: machine.Validate},
		compilation.Rules{Normalize: substrate.Normalize, ValidateAuthored: substrate.ValidateAuthored, ValidatePartial: substrate.ValidatePartial, Validate: substrate.Validate},
		compilation.Rules{Normalize: managedos.Normalize, ValidateAuthored: managedos.ValidateAuthored, ValidatePartial: managedos.ValidatePartial, Validate: managedos.Validate},
		compilation.Rules{Normalize: containercluster.Normalize, ValidateAuthored: containercluster.ValidateAuthored, ValidatePartial: containercluster.ValidatePartial, Validate: containercluster.Validate},
		compilation.Rules{Normalize: infrastructureservices.Normalize, ValidateAuthored: infrastructureservices.ValidateAuthored, ValidatePartial: infrastructureservices.ValidatePartial, Validate: infrastructureservices.Validate})
}

func wireDesiredState(deps serviceDependencies, compiler compilation.Compiler) cli.DesiredStateService {
	inputs := contexts.Inputs{Repository: deps.Repository, Selection: deps.Selection}
	return compilation.New(inputReader(deps.Files), compiler, inputs)
}

// inputReader reads operator input through the invoking account's opener, and
// binds no opener at all when files is nil rather than a wrapper around nil.
func inputReader(files operatorFiles) inputfs.Reader {
	if files == nil {
		return inputfs.Reader{}
	}
	return inputfs.Reader{Files: inputFiles{files: files}}
}

type inputFiles struct{ files operatorFiles }

func (f inputFiles) Begin(ctx context.Context) (inputfs.FileSession, error) {
	session, err := f.files.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}
