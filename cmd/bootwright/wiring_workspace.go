package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/desiredstate/inputfs"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func wireContexts(deps serviceDependencies, compiler compilation.Compiler, secrets contextSecretHooks) cli.ContextService {
	return contexts.New(inputfs.Reader{}, compiler, deps.Repository, contextguard.Guard{}, deps.Confirmer, contexts.Options{
		Selection:             deps.Selection,
		ConfigurationReader:   inputfs.Reader{},
		ValidateConfiguration: secrets.Validate,
		InitializeSecrets:     secrets.Initialize,
	})
}
