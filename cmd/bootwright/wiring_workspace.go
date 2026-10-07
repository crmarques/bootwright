package main

import (
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// wireContexts shows an update's or a deletion's plan on the process's standard
// output, and its warnings on standard error, ahead of the prompt there. A
// process with no output binds no presenter, so the service refuses to ask
// rather than ask blind.
func wireContexts(deps serviceDependencies, compiler compilation.Compiler, secrets contextSecretHooks) cli.ContextService {
	reader := inputReader(deps.Files)
	options := contexts.Options{
		Selection:             deps.Selection,
		ConfigurationReader:   reader,
		ValidateConfiguration: secrets.Validate,
		InitializeSecrets:     secrets.Initialize,
	}
	if deps.Streams.Out != nil {
		options.Presenter = cli.NewContextPlanPresenter(deps.Streams.Out, deps.Streams.Err)
	}
	return contexts.New(reader, compiler, deps.Repository, contextguard.Guard{}, deps.Confirmer, options)
}
