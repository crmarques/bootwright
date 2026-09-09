package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/invocation"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/secrets/storage"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/crmarques/bootwright/internal/workspace/selectionfs"
)

type accountResolver interface {
	Resolve(context.Context) (invocation.Account, error)
}

// Account resolution and user-file access are lazy, so constructing services for
// help, completion and context-free validation acquires no account capability.
type invokingAccount struct{ resolver accountResolver }

func (a invokingAccount) selection(ctx context.Context) (contexts.SelectionStore, error) {
	account, err := a.resolver.Resolve(ctx)
	if err != nil {
		return nil, contexts.StateError("invoking account cannot be verified")
	}
	executable, err := invocation.Executable()
	if err != nil {
		return nil, contexts.StateError("selection helper executable cannot be verified")
	}
	return selectionfs.New(selectionfs.Options{UID: account.UID, GID: account.GID, Home: account.Home, Groups: account.Groups, Executable: executable}), nil
}

func (a invokingAccount) Read(ctx context.Context) (contexts.Selection, error) {
	store, err := a.selection(ctx)
	if err != nil {
		return contexts.Selection{}, err
	}
	return store.Read(ctx)
}

func (a invokingAccount) Write(ctx context.Context, selection contexts.Selection) error {
	store, err := a.selection(ctx)
	if err != nil {
		return err
	}
	return store.Write(ctx, selection)
}

func (a invokingAccount) Clear(ctx context.Context, selection contexts.Selection) error {
	store, err := a.selection(ctx)
	if err != nil {
		return err
	}
	return store.Clear(ctx, selection)
}

func (a invokingAccount) FileIdentity(ctx context.Context) (material.FileIdentity, error) {
	account, err := a.resolver.Resolve(ctx)
	if err != nil {
		return material.FileIdentity{}, contexts.StateError("invoking account cannot be verified")
	}
	return material.FileIdentity{UID: account.UID, Home: account.Home}, nil
}

type boundedFileReader interface {
	ReadFile(context.Context, string, int) ([]byte, error)
}

type contextConfigurationReader struct{ reader boundedFileReader }

func (r contextConfigurationReader) ReadConfiguration(ctx context.Context, path string) ([]byte, error) {
	return r.reader.ReadFile(ctx, path, contexts.MaxConfigurationBytes)
}

type selectionWorkspace struct {
	storage.Workspace
	selection contexts.SelectionStore
}

func (w selectionWorkspace) SecretContext(ctx context.Context, name string) (storage.ContextSnapshot, error) {
	var selected contexts.Selection
	if name == "" {
		if w.selection == nil {
			return storage.ContextSnapshot{}, contexts.StateError("current context selection is not configured")
		}
		var err error
		selected, err = w.selection.Read(ctx)
		if err != nil {
			return storage.ContextSnapshot{}, err
		}
		if selected.Name == "" {
			return storage.ContextSnapshot{}, contexts.StateError("no current context; run context use --name <name>")
		}
		name = selected.Name
	}
	if w.Workspace == nil {
		return storage.ContextSnapshot{}, contexts.StateError("secret workspace is not configured")
	}
	snapshot, err := w.Workspace.SecretContext(ctx, name)
	if err != nil {
		return storage.ContextSnapshot{}, err
	}
	if selected.ID != "" && snapshot.Context.ID != selected.ID {
		return storage.ContextSnapshot{}, contexts.StateError("current context selection is stale; run context use --name " + name)
	}
	return snapshot, nil
}
