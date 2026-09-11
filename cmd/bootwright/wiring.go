package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// processDependencies carries the capabilities only an interactive process can
// supply. Its zero value builds the services used by help, completion, version
// and explicit-input validation, which acquire no terminal, account or process
// capability.
type processDependencies struct {
	Confirmer   contexts.Confirmer
	SecretInput material.InputReader
	Progress    prerequisites.ProgressReporter
	Presenter   prerequisites.PlanPresenter
}

// serviceDependencies names every replaceable implementation the application
// services consume. Each field is a consumer-owned contract, so a test binds
// the same graph through its own implementations.
type serviceDependencies struct {
	Repository      contexts.Repository
	Workspace       secretstore.Workspace
	Selection       contexts.SelectionStore
	Operator        material.Operator
	Confirmer       contexts.Confirmer
	SecretInput     material.InputReader
	Resolver        secretstore.ImplementationResolver
	SessionMaterial secretstore.SessionMaterialSource
	Controller      controllerDependencies
}

func wireServices(process processDependencies) cli.Services {
	repository := contextfs.New(contextfs.Options{})
	account := invokingAccount{resolver: privilege.Resolver{}}
	return assembleServices(serviceDependencies{
		Repository:  repository,
		Workspace:   repository,
		Selection:   account,
		Operator:    account,
		Confirmer:   process.Confirmer,
		SecretInput: process.SecretInput,
		Controller:  localControllerDependencies(repository, process),
	})
}

// assembleServices is the complete service graph. Secrets is assembled first
// because context initialization consumes its transaction-scoped hooks.
func assembleServices(deps serviceDependencies) cli.Services {
	compiler := wireCompiler()
	secrets := wireSecrets(deps, compiler)
	services := wireStubs()
	services.Contexts = wireContexts(deps, compiler, secrets.hooks)
	services.Secrets, services.Encryption = secrets.custody, secrets.encryption
	services.DesiredState = wireDesiredState(deps, compiler)
	services.Controller = wireController(deps.Controller, compiler, deps.Confirmer)
	return services
}

type secretInputFunc func(context.Context, []byte) (int, error)

func (f secretInputFunc) Read(ctx context.Context, buffer []byte) (int, error) { return f(ctx, buffer) }
