package main

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/crmarques/bootwright/internal/workspace/invokerfs"
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
	return selectionfs.New(selectionfs.Options{UID: account.UID, GID: account.GID, Home: account.Home, Groups: account.Groups}), nil
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

// uid resolves the invoking account's user ID from the account database. A
// session calls it only when an offered key file must be proved that
// account's own, so an invocation that offers none acquires no account
// capability.
func (a invokingAccount) uid() (int, error) {
	account, err := a.resolver.Resolve(context.Background())
	if err != nil {
		return 0, contexts.StateError("invoking account cannot be verified")
	}
	return account.UID, nil
}

// files opens operator-named paths with the invoking account's credentials.
// Only a root process beginning a session resolves the account.
func (a invokingAccount) files() *invokerfs.Opener {
	return invokerfs.New(func(ctx context.Context) (invokerfs.Account, error) {
		account, err := a.resolver.Resolve(ctx)
		if err != nil {
			return invokerfs.Account{}, errors.New("invoking account cannot be verified")
		}
		return invokerfs.Account{UID: account.UID, GID: account.GID, Groups: append([]uint32(nil), account.Groups...)}, nil
	})
}

// openerFiles binds the invoking account's opener to the composition root's
// file port.
type openerFiles struct{ opener *invokerfs.Opener }

func (f openerFiles) Begin(ctx context.Context) (operatorFileSession, error) {
	session, err := f.opener.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (a invokingAccount) FileIdentity(ctx context.Context) (material.FileIdentity, error) {
	account, err := a.resolver.Resolve(ctx)
	if err != nil {
		return material.FileIdentity{}, contexts.StateError("invoking account cannot be verified")
	}
	return material.FileIdentity{UID: account.UID, Home: account.Home}, nil
}
