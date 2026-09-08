package custody

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/secrets"
)

type Service struct{}

type SetRequest struct {
	ContextName      string
	Name             string
	Source           secrets.Source
	Username         string
	SkipConfirmation bool
}

func (Service) Set(ctx context.Context, _ SetRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type GenerateRequest struct {
	ContextName string
	Renew       bool
}

func (Service) Generate(ctx context.Context, _ GenerateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type CheckRequest struct {
	ContextName string
}

func (Service) Check(ctx context.Context, _ CheckRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type ListRequest struct {
	ContextName string
}

func (Service) List(ctx context.Context, _ ListRequest) error {
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

func (Service) Show(ctx context.Context, _ ShowRequest) error {
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

func (Service) Delete(ctx context.Context, _ DeleteRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
