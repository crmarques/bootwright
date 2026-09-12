package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/localkeyring"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// contextSecretHooks lets context creation validate its configured secret
// implementation and initialize it inside the same transaction, without
// reacquiring the store lock.
type contextSecretHooks struct {
	Validate   func(context.Context, contexts.Configuration) error
	Initialize func(context.Context, contexts.Record, secretstore.Area) error
}

type secretServices struct {
	custody    cli.SecretService
	encryption cli.EncryptionService
	binder     *custody.Service
	hooks      contextSecretHooks
}

func wireSecrets(deps serviceDependencies, compiler compilation.Compiler) secretServices {
	resolver := deps.Resolver
	if resolver == nil {
		resolver = secretstore.NewCatalog(localkeyring.New())
	}
	selected := contexts.SelectionWorkspace{Workspace: deps.Workspace, Selection: deps.Selection}
	access := secretstore.NewAccess(selected, resolver, deps.SessionMaterial)
	acquisition := material.New(deps.SecretInput, material.Options{Operator: deps.Operator})
	secrets := custody.New(access, compiler, acquisition, deps.Confirmer)
	return secretServices{
		custody:    secrets,
		binder:     secrets,
		encryption: encryption.New(access, deps.Confirmer),
		hooks: contextSecretHooks{
			Validate: func(ctx context.Context, configuration contexts.Configuration) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				_, err := resolver.Select(configuration.SecretStore.Type)
				return err
			},
			Initialize: func(ctx context.Context, record contexts.Record, area secretstore.Area) error {
				selected := secretstore.Context{Name: record.Name, ID: record.ID, Mode: string(record.Mode), Revision: record.Revision}
				return access.InitializeArea(ctx, selected, record.SecretStoreType, area)
			},
		},
	}
}
