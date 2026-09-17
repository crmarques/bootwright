package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/crmarques/bootwright/internal/workspace/selectionfs"
)

type accountResolver interface {
	Resolve(context.Context) (privilege.Account, error)
}

// Account resolution and user-file access are lazy, so constructing services for
// help, completion and context-free validation acquires no account capability.
type invokingAccount struct{ resolver accountResolver }

func (a invokingAccount) selection(ctx context.Context) (contexts.SelectionStore, error) {
	account, err := a.resolver.Resolve(ctx)
	if err != nil {
		return nil, contexts.StateError("invoking account cannot be verified")
	}
	executable, err := privilege.Executable()
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

// home resolves the invoking account's home directory from the account
// database. A session calls it only when an offered key path needs expanding,
// so an invocation that offers none acquires no account capability.
func (a invokingAccount) home() (string, error) {
	account, err := a.resolver.Resolve(context.Background())
	if err != nil {
		return "", contexts.StateError("invoking account cannot be verified")
	}
	return account.Home, nil
}

func (a invokingAccount) FileIdentity(ctx context.Context) (material.FileIdentity, error) {
	account, err := a.resolver.Resolve(ctx)
	if err != nil {
		return material.FileIdentity{}, contexts.StateError("invoking account cannot be verified")
	}
	return material.FileIdentity{UID: account.UID, Home: account.Home}, nil
}
