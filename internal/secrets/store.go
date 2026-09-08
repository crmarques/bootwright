package secrets

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Store struct{}

type SetRequest struct {
	ContextName      string
	Name             string
	Source           Source
	Username         string
	SkipConfirmation bool
}

func (Store) Set(ctx context.Context, _ SetRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type GenerateRequest struct {
	ContextName string
	Renew       bool
}

func (Store) Generate(ctx context.Context, _ GenerateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type CheckRequest struct {
	ContextName string
}

func (Store) Check(ctx context.Context, _ CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ListRequest struct {
	ContextName string
}

func (Store) List(ctx context.Context, _ ListRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ShowRequest struct {
	ContextName string
	Name        string
	Part        string
}

func (Store) Show(ctx context.Context, _ ShowRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type DeleteRequest struct {
	ContextName      string
	Name             string
	SkipConfirmation bool
}

func (Store) Delete(ctx context.Context, _ DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
