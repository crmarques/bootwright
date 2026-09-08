package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
)

func secretCommands() []commandSpec {
	return []commandSpec{
		{path: "secret set", short: "Store confidential material", flags: []flagSpec{nameFlag(), stringFlag("pull-secret", "Read a pull-secret file"), stringFlag("tls-cert", "Read a TLS certificate file"), stringFlag("tls-key", "Read the paired TLS private-key file"), stringFlag("raw-file", "Read raw material from a file"), stringFlag("from-file", "Read material from a file"), boolFlag("password-stdin", "Read a password from standard input"), boolFlag("generate", "Generate secret material"), stringFlag("username", "Associate a username with secret material"), confirmationFlag()}, long: "Store one secret using exactly one source mode. --tls-cert and --tls-key are an inseparable pair. --password-stdin requires --username."},
		{path: "secret generate", short: "Generate missing declared secrets", flags: []flagSpec{boolFlag("renew", "Renew declarations with a generated source")}},
		{path: "secret check", short: "Check secret availability and types", flags: []flagSpec{outputFlag()}},
		{path: "secret list", short: "List secret metadata", flags: []flagSpec{outputFlag()}},
		{path: "secret show", short: "Export raw sensitive secret bytes to standard output", flags: []flagSpec{nameFlag(), enumFlag("part", "primary", "Select the secret part", "primary", "private", "public", "tls-key")}},
		{path: "secret delete", short: "Delete an unbound secret", flags: []flagSpec{nameFlag(), confirmationFlag()}},
		{path: "secret encryption init", short: "Initialize the encryption keyring"},
		{path: "secret encryption status", short: "Show encryption metadata", flags: []flagSpec{outputFlag()}},
		{path: "secret encryption rotate", short: "Rotate the active encryption key", flags: []flagSpec{confirmationFlag()}},
	}
}

type SecretService interface {
	Set(context.Context, custody.SetRequest) error
	Generate(context.Context, custody.GenerateRequest) error
	Check(context.Context, custody.CheckRequest) error
	List(context.Context, custody.ListRequest) error
	Show(context.Context, custody.ShowRequest) error
	Delete(context.Context, custody.DeleteRequest) error
}

type EncryptionService interface {
	Init(context.Context, encryption.EncryptionInitRequest) error
	Status(context.Context, encryption.EncryptionStatusRequest) error
	Rotate(context.Context, encryption.EncryptionRotateRequest) error
}

func (s Services) invokeSecrets(ctx context.Context, path string, values *requestValues, args []string) error {
	if s.Secrets == nil {
		return errMissingService
	}
	switch path {
	case "secret set":
		return invokeRequest(ctx, values, custody.SetRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			Source:           values.secretSource(),
			Username:         values.text("username"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Secrets.Set)
	case "secret generate":
		return invokeRequest(ctx, values, custody.GenerateRequest{
			ContextName: values.text("context"),
			Renew:       values.boolean("renew"),
		}, s.Secrets.Generate)
	case "secret check":
		return invokeRequest(ctx, values, custody.CheckRequest{
			ContextName: values.text("context"),
		}, s.Secrets.Check)
	case "secret list":
		return invokeRequest(ctx, values, custody.ListRequest{
			ContextName: values.text("context"),
		}, s.Secrets.List)
	case "secret show":
		return invokeRequest(ctx, values, custody.ShowRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Part:        values.text("part"),
		}, s.Secrets.Show)
	case "secret delete":
		return invokeRequest(ctx, values, custody.DeleteRequest{
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
		return invokeRequest(ctx, values, encryption.EncryptionInitRequest{
			ContextName: values.text("context"),
		}, s.Encryption.Init)
	case "secret encryption status":
		return invokeRequest(ctx, values, encryption.EncryptionStatusRequest{
			ContextName: values.text("context"),
		}, s.Encryption.Status)
	case "secret encryption rotate":
		return invokeRequest(ctx, values, encryption.EncryptionRotateRequest{
			ContextName:      values.text("context"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Encryption.Rotate)
	default:
		return errors.New("command has no application dispatch")
	}
}
