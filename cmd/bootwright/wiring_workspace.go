package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation/contextguard"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
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
	if deps.Lifecycle.Workspace != nil {
		options.Operations = unfinishedOperations{workspace: deps.Lifecycle.Workspace, operations: operationStores(systemClock{}, deps.Lifecycle.Executable)}
	}
	if deps.Streams.Out != nil {
		options.Presenter = cli.NewContextPlanPresenter(deps.Streams.Out, deps.Streams.Err)
	}
	return contexts.New(reader, compiler, deps.Repository, contextguard.Guard{}, deps.Confirmer, options)
}

type unfinishedOperations struct {
	workspace  lifecycle.Workspace
	operations lifecycle.OperationStoreFactory
}

func (u unfinishedOperations) Unfinished(ctx context.Context, name string) (*contexts.Unfinished, error) {
	found, unfinished, err := lifecycle.ReadUnfinished(ctx, u.workspace, u.operations, name)
	if err != nil || !unfinished {
		return nil, err
	}
	return &contexts.Unfinished{Operation: found.ID, Verb: found.Verb, State: found.State, Build: found.Build}, nil
}
