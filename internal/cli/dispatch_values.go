package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

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

func (v *requestValues) strings(name string) []string {
	value, err := v.flags.GetStringArray(name)
	v.recordFirstError(err)
	return slices.Clone(value)
}

func (v *requestValues) duration(name string) time.Duration {
	value, err := time.ParseDuration(v.text(name))
	v.recordFirstError(err)
	return value
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
	if len(files) != 1 {
		v.recordFirstError(errors.New("context input requires exactly one directory"))
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

func (v *requestValues) secretSource() secrets.Source {
	switch {
	case v.text("pull-secret") != "":
		return secrets.Source{Kind: secrets.PullSecretSource, File: v.text("pull-secret")}
	case v.text("tls-cert") != "":
		return secrets.Source{Kind: secrets.TLSSource, CertificateFile: v.text("tls-cert"), PrivateKeyFile: v.text("tls-key")}
	case v.text("raw-file") != "":
		return secrets.Source{Kind: secrets.RawFileSource, File: v.text("raw-file")}
	case v.text("from-file") != "":
		return secrets.Source{Kind: secrets.StructuredFileSource, File: v.text("from-file")}
	case v.boolean("password-stdin"):
		return secrets.Source{Kind: secrets.PasswordStdinSource}
	case v.boolean("generate"):
		return secrets.Source{Kind: secrets.GeneratedSource}
	default:
		v.recordFirstError(errors.New("secret source is not configured"))
		return secrets.Source{}
	}
}
