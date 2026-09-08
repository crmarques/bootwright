package encryption

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Service struct{}

type EncryptionInitRequest struct {
	ContextName string
}

func (Service) Init(ctx context.Context, _ EncryptionInitRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type EncryptionStatusRequest struct {
	ContextName string
}

func (Service) Status(ctx context.Context, _ EncryptionStatusRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type EncryptionRotateRequest struct {
	ContextName      string
	SkipConfirmation bool
}

func (Service) Rotate(ctx context.Context, _ EncryptionRotateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
