package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/spf13/pflag"
)

func invokeRequest[T any](ctx context.Context, values *requestValues, request T, invoke func(context.Context, T) error) error {
	if values.err != nil {
		return fmt.Errorf("invalid application request wiring: %w", values.err)
	}
	return invoke(ctx, request)
}

func invokeResult[T, R any](ctx context.Context, values *requestValues, request T, invoke func(context.Context, T) (*R, error)) (*R, error) {
	if values.err != nil {
		return nil, fmt.Errorf("invalid application request wiring: %w", values.err)
	}
	return invoke(ctx, request)
}

type requestValues struct {
	flags *pflag.FlagSet
	err   error
}

func (v *requestValues) recordFirstError(err error) {
	if v.err == nil {
		v.err = err
	}
}

func (v *requestValues) text(name string) string {
	value, err := v.flags.GetString(name)
	v.recordFirstError(err)
	return value
}

func (v *requestValues) boolean(name string) bool {
	value, err := v.flags.GetBool(name)
	v.recordFirstError(err)
	return value
}

// optionalBoolean reads a flag only the commands that offer it declare, so one
// shared request shape can carry an arm its sibling command does not take.
func (v *requestValues) optionalBoolean(name string) bool {
	if v.flags.Lookup(name) == nil {
		return false
	}
	return v.boolean(name)
}

func (v *requestValues) strings(name string) []string {
	value, err := v.flags.GetStringArray(name)
	v.recordFirstError(err)
	return slices.Clone(value)
}

func (v *requestValues) names(name string) []string {
	value := v.text(name)
	if value == "" {
		return nil
	}
	if value == "," {
		return []string{}
	}
	return strings.Split(value, ",")
}

func (v *requestValues) singleFile() string {
	files := v.strings("file")
	if len(files) > 1 {
		v.recordFirstError(errors.New("context configuration accepts at most one file"))
		return ""
	}
	if len(files) == 0 {
		return ""
	}
	return files[0]
}

func (v *requestValues) validationContext() string {
	if len(v.strings("file")) != 0 {
		return ""
	}
	return v.text("context")
}

func (v *requestValues) addOnName() string {
	name, _, _ := strings.Cut(v.text("name"), ":")
	return name
}

func (v *requestValues) addOnVersion() string {
	_, version, found := strings.Cut(v.text("name"), ":")
	if found {
		return version
	}
	if v.flags.Lookup("version") == nil {
		return ""
	}
	return v.text("version")
}

func (v *requestValues) authorizations() []string {
	var tokens []string
	for _, value := range v.strings("authorize") {
		for token := range strings.SplitSeq(value, ",") {
			tokens = append(tokens, strings.TrimSpace(token))
		}
	}
	return tokens
}

func (v *requestValues) ssh() machine.SSHOptions {
	return machine.SSHOptions{
		IdentityFile:       v.text("ssh-id-file"),
		User:               v.text("ssh-user"),
		AskSudoPassword:    v.boolean("ssh-ask-sudo-password"),
		UserForProvisioned: v.boolean("ssh-user-for-provisioned"),
	}
}

func (v *requestValues) secretInput() secrets.Input {
	var provided secrets.InputFields
	for _, input := range []struct {
		name  string
		field secrets.InputFields
	}{
		{"value-file", secrets.ValueFileInput},
		{"value-stdin", secrets.ValueStdinInput},
		{"username", secrets.UsernameInput},
		{"password-file", secrets.PasswordFileInput},
		{"password-stdin", secrets.PasswordStdinInput},
		{"certificate-file", secrets.CertificateFileInput},
		{"private-key-file", secrets.PrivateKeyFileInput},
		{"public-key-file", secrets.PublicKeyFileInput},
	} {
		if v.flags.Changed(input.name) {
			provided |= input.field
		}
	}
	return secrets.Input{
		Provided:        provided,
		ValueFile:       v.text("value-file"),
		ValueStdin:      v.boolean("value-stdin"),
		Username:        v.text("username"),
		PasswordFile:    v.text("password-file"),
		PasswordStdin:   v.boolean("password-stdin"),
		CertificateFile: v.text("certificate-file"),
		PrivateKeyFile:  v.text("private-key-file"),
		PublicKeyFile:   v.text("public-key-file"),
	}
}
