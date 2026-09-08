package secrets

import (
	"context"

	"github.com/crmarques/bootwright/internal/availability"
)

type Encryption struct{}

type EncryptionInitRequest struct {
	ContextName string
}

func (Encryption) Init(ctx context.Context, _ EncryptionInitRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type EncryptionStatusRequest struct {
	ContextName string
}

func (Encryption) Status(ctx context.Context, _ EncryptionStatusRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}

type EncryptionRotateRequest struct {
	ContextName      string
	SkipConfirmation bool
}

func (Encryption) Rotate(ctx context.Context, _ EncryptionRotateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return availability.ErrNotImplemented
}
