package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/secrets"
)

func (s Services) invokeSecrets(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Secrets == nil {
		return errMissingService
	}
	switch path {
	case "secret set":
		return invokeRequest(ctx, values, secrets.SetRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			Source:           values.secretSource(),
			Username:         values.text("username"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Secrets.Set)
	case "secret generate":
		return invokeRequest(ctx, values, secrets.GenerateRequest{
			ContextName: values.text("context"),
			Renew:       values.boolean("renew"),
		}, s.Secrets.Generate)
	case "secret check":
		return invokeRequest(ctx, values, secrets.CheckRequest{
			ContextName: values.text("context"),
		}, s.Secrets.Check)
	case "secret list":
		return invokeRequest(ctx, values, secrets.ListRequest{
			ContextName: values.text("context"),
		}, s.Secrets.List)
	case "secret show":
		return invokeRequest(ctx, values, secrets.ShowRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Part:        values.text("part"),
		}, s.Secrets.Show)
	case "secret delete":
		return invokeRequest(ctx, values, secrets.DeleteRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Secrets.Delete)
	default:
		return errors.New("command has no application dispatch")
	}
}

func (s Services) invokeEncryption(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Encryption == nil {
		return errMissingService
	}
	switch path {
	case "secret encryption init":
		return invokeRequest(ctx, values, secrets.EncryptionInitRequest{
			ContextName: values.text("context"),
		}, s.Encryption.Init)
	case "secret encryption status":
		return invokeRequest(ctx, values, secrets.EncryptionStatusRequest{
			ContextName: values.text("context"),
		}, s.Encryption.Status)
	case "secret encryption rotate":
		return invokeRequest(ctx, values, secrets.EncryptionRotateRequest{
			ContextName:      values.text("context"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Encryption.Rotate)
	default:
		return errors.New("command has no application dispatch")
	}
}
