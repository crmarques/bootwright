package cli

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
)

func secretCommands() []commandSpec {
	return []commandSpec{
		available(commandSpec{path: "secret set", validate: validateSecretSet, classify: classifyRules{secretStdin: secretStdin}, example: "--name token --value-file ./token --yes", invoke: Services.invokeSecretSet, short: "Store confidential material", flags: []flagSpec{nameFlag(), fileFlag("value-file", "Read a value from a file"), boolFlag("value-stdin", "Read a value from standard input"), stringFlag("username", "Store this username with a password"), fileFlag("password-file", "Read a password from a file"), boolFlag("password-stdin", "Read a password from standard input"), fileFlag("certificate-file", "Read a certificate or CA bundle from a file"), fileFlag("private-key-file", "Read a private key from a file"), fileFlag("public-key-file", "Read an SSH public key from a file"), confirmationFlag()}, long: "Store one declared contextStore secret using the exact input flags for its type. Stdin-backed replacement requires --yes."}),
		available(commandSpec{path: "secret generate", example: "--name ssh-key", invoke: Services.invokeSecretGenerate, short: "Generate declared secret material", flags: []flagSpec{stringFlag("name", "Select one generated secret (default: all)"), boolFlag("renew", "Renew selected generated material"), confirmationFlag()}, long: "Create missing or stale generated secrets; --renew replaces current ones after confirmation unless --yes. A Secret whose current version a live operation holds as effect material is refused when named and left unchanged otherwise."}),
		available(commandSpec{path: "secret check", example: "--output json", invoke: Services.invokeSecretCheck, short: "Check secret availability and types"}),
		available(commandSpec{path: "secret info", validate: validateInfoNames("secret"), example: "--context lab --name ssh-key", invoke: Services.invokeSecretInfo, short: "Inspect secret identity and custody metadata", flags: []flagSpec{stringFlag("name", "Select comma-separated secret names (default: all)")}, long: "Show stored secret identity, parts, and custody metadata. --name takes a comma-separated list (default: all). This command reads metadata only; it never reads or reveals secret material."}),
		available(commandSpec{path: "secret list", example: "--output json", invoke: Services.invokeSecretList, short: "List secret metadata"}),
		available(commandSpec{path: "secret show", example: "--name token --part value", invoke: Services.invokeSecretShow, short: "Export raw sensitive secret bytes to standard output", flags: []flagSpec{nameFlag(), requiredEnumFlag("part", "Select the secret part", "value", "username", "password", "certificate", "private-key", "public-key")}}),
		available(commandSpec{path: "secret delete", example: "--name token", invoke: Services.invokeSecretDelete, short: "Remove an active secret mapping, retaining bound versions", flags: []flagSpec{nameFlag(), confirmationFlag()}}),
		available(commandSpec{path: "secret encryption init", example: "", invoke: Services.invokeEncryptionInit, short: "Initialize or recover the configured confidential store"}),
		available(commandSpec{path: "secret encryption status", example: "", invoke: Services.invokeEncryptionStatus, short: "Show encryption metadata"}),
		available(commandSpec{path: "secret encryption rotate", example: "", invoke: Services.invokeEncryptionRotate, short: "Rotate the active encryption key", flags: []flagSpec{confirmationFlag()}}),
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
	Init(context.Context, encryption.EncryptionInitRequest) (*encryption.MutationResult, error)
	Status(context.Context, encryption.EncryptionStatusRequest) (*encryption.StatusResult, error)
	Rotate(context.Context, encryption.EncryptionRotateRequest) (*encryption.MutationResult, error)
}

func (s Services) invokeSecretSet(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.SetRequest{
		ContextName:      values.text("context"),
		Name:             values.text("name"),
		Input:            values.secretInput(),
		SkipConfirmation: values.boolean("yes"),
	}, s.Secrets.Set)
	return commandResult{secretMutation: result}, err
}

func (s Services) invokeSecretGenerate(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.GenerateRequest{
		ContextName:      values.text("context"),
		Name:             values.text("name"),
		Renew:            values.boolean("renew"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Secrets.Generate)
	return commandResult{secretMutation: result}, err
}

func (s Services) invokeSecretCheck(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.CheckRequest{
		ContextName: values.text("context"),
	}, s.Secrets.Check)
	return commandResult{secretCheck: result}, err
}

func (s Services) invokeSecretList(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.ListRequest{
		ContextName: values.text("context"),
	}, s.Secrets.List)
	return commandResult{secretList: result}, err
}

func (s Services) invokeSecretInfo(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.ListRequest{ContextName: values.text("context"), Names: selectedInfoNames(values)}, s.Secrets.List)
	return commandResult{secretList: result}, err
}

func (s Services) invokeSecretShow(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.ShowRequest{
		ContextName: values.text("context"),
		Name:        values.text("name"),
		Part:        secrets.Part(values.text("part")),
	}, s.Secrets.Show)
	return commandResult{secretReveal: result}, err
}

func (s Services) invokeSecretDelete(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Secrets == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, custody.DeleteRequest{
		ContextName:      values.text("context"),
		Name:             values.text("name"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Secrets.Delete)
	return commandResult{secretMutation: result}, err
}

func (s Services) invokeEncryptionInit(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Encryption == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, encryption.EncryptionInitRequest{
		ContextName: values.text("context"),
	}, s.Encryption.Init)
	return commandResult{encryptionMutation: result}, err
}

func (s Services) invokeEncryptionStatus(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Encryption == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, encryption.EncryptionStatusRequest{
		ContextName: values.text("context"),
	}, s.Encryption.Status)
	return commandResult{encryptionStatus: result}, err
}

func (s Services) invokeEncryptionRotate(ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
	if s.Encryption == nil {
		return commandResult{}, errMissingService
	}
	result, err := invokeResult(ctx, values, encryption.EncryptionRotateRequest{
		ContextName:      values.text("context"),
		SkipConfirmation: values.boolean("yes"),
	}, s.Encryption.Rotate)
	return commandResult{encryptionMutation: result}, err
}
