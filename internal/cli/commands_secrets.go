package cli

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
)

func secretCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "secret set", short: "Store confidential material", flags: []flagSpec{nameFlag(), fileFlag("value-file", "Read a value from a file"), boolFlag("value-stdin", "Read a value from standard input"), stringFlag("username", "Store this username with a password"), fileFlag("password-file", "Read a password from a file"), boolFlag("password-stdin", "Read a password from standard input"), fileFlag("certificate-file", "Read a certificate or CA bundle from a file"), fileFlag("private-key-file", "Read a private key from a file"), fileFlag("public-key-file", "Read an SSH public key from a file"), confirmationFlag()}, long: "Store one declared contextStore secret using the exact input flags for its type. Stdin-backed replacement requires --yes."}),
		available(commandSpec{path: "secret generate", short: "Generate declared secret material", flags: []flagSpec{stringFlag("name", "Select one generated secret (default: all)"), boolFlag("renew", "Renew selected generated material")}}),
		available(commandSpec{path: "secret check", short: "Check secret availability and types", flags: []flagSpec{outputFlag()}}),
		available(commandSpec{path: "secret list", short: "List secret metadata", flags: []flagSpec{outputFlag()}}),
		available(commandSpec{path: "secret show", short: "Export raw sensitive secret bytes to standard output", flags: []flagSpec{nameFlag(), requiredEnumFlag("part", "Select the secret part", "value", "username", "password", "certificate", "private-key", "public-key")}}),
		available(commandSpec{path: "secret delete", short: "Remove an active secret mapping, retaining bound versions", flags: []flagSpec{nameFlag(), confirmationFlag()}}),
		available(commandSpec{path: "secret encryption init", short: "Initialize or recover the configured confidential store"}),
		available(commandSpec{path: "secret encryption status", short: "Show encryption metadata", flags: []flagSpec{outputFlag()}}),
		available(commandSpec{path: "secret encryption rotate", short: "Rotate the active encryption key", flags: []flagSpec{confirmationFlag()}}),
	}
}

type SecretService interface {
	Set(context.Context, custody.SetRequest) (*custody.MutationResult, error)
	Generate(context.Context, custody.GenerateRequest) (*custody.MutationResult, error)
	Check(context.Context, custody.CheckRequest) (*custody.CheckResult, error)
	List(context.Context, custody.ListRequest) (*custody.ListResult, error)
	Show(context.Context, custody.ShowRequest) (*custody.RevealResult, error)
	Delete(context.Context, custody.DeleteRequest) (*custody.MutationResult, error)
}

type EncryptionService interface {
	Types() []string
	Init(context.Context, encryption.EncryptionInitRequest) (*encryption.MutationResult, error)
	Status(context.Context, encryption.EncryptionStatusRequest) (*encryption.StatusResult, error)
	Rotate(context.Context, encryption.EncryptionRotateRequest) (*encryption.MutationResult, error)
}

func (s Services) invokeSecrets(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "secret set":
		result, err := invokeResult(ctx, values, custody.SetRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			Input:            values.secretInput(),
			SkipConfirmation: values.boolean("yes"),
		}, s.Secrets.Set)
		return commandResult{secretMutation: result}, err
	case "secret generate":
		result, err := invokeResult(ctx, values, custody.GenerateRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Renew:       values.boolean("renew"),
		}, s.Secrets.Generate)
		return commandResult{secretMutation: result}, err
	case "secret check":
		result, err := invokeResult(ctx, values, custody.CheckRequest{
			ContextName: values.text("context"),
		}, s.Secrets.Check)
		return commandResult{secretCheck: result}, err
	case "secret list":
		result, err := invokeResult(ctx, values, custody.ListRequest{
			ContextName: values.text("context"),
		}, s.Secrets.List)
		return commandResult{secretList: result}, err
	case "secret show":
		result, err := invokeResult(ctx, values, custody.ShowRequest{
			ContextName: values.text("context"),
			Name:        values.text("name"),
			Part:        secrets.Part(values.text("part")),
		}, s.Secrets.Show)
		return commandResult{secretReveal: result}, err
	case "secret delete":
		result, err := invokeResult(ctx, values, custody.DeleteRequest{
			ContextName:      values.text("context"),
			Name:             values.text("name"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Secrets.Delete)
		return commandResult{secretMutation: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}

func (s Services) invokeEncryption(ctx context.Context, path string, values *requestValues, args []string) (commandResult, error) {
	if s.Encryption == nil {
		return commandResult{}, errMissingService
	}
	switch path {
	case "secret encryption init":
		result, err := invokeResult(ctx, values, encryption.EncryptionInitRequest{
			ContextName: values.text("context"),
		}, s.Encryption.Init)
		return commandResult{encryptionMutation: result}, err
	case "secret encryption status":
		result, err := invokeResult(ctx, values, encryption.EncryptionStatusRequest{
			ContextName: values.text("context"),
		}, s.Encryption.Status)
		return commandResult{encryptionStatus: result}, err
	case "secret encryption rotate":
		result, err := invokeResult(ctx, values, encryption.EncryptionRotateRequest{
			ContextName:      values.text("context"),
			SkipConfirmation: values.boolean("yes"),
		}, s.Encryption.Rotate)
		return commandResult{encryptionMutation: result}, err
	default:
		return commandResult{}, errors.New("command has no application dispatch")
	}
}
