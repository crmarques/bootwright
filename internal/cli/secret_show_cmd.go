package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newSecretShowCmd(stdout io.Writer, stderr io.Writer) *cobra.Command {
	var name string
	part := "primary"
	cmd := &cobra.Command{
		Use:   "show --name <secret-name>",
		Short: "Print context-local secret material",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&name, "name", "", "context-local secret name to print (required)")
	cmd.Flags().StringVar(&part, "part", part, "secret material part (primary|private|public|tls-key)")
	_ = cmd.MarkFlagRequired("name")
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		if !desiredstate.IsDNSLabel(name) {
			return failf(2, "--name must be a lowercase DNS label")
		}
		role, err := localSecretPartRole(part)
		if err != nil {
			return failErr(2, err)
		}
		store := secret.NewContextStore(ctx.Name, ctx.SecretsDir)
		key := secret.MaterialKey{Name: name, Role: role}
		status, err := store.Inspect(key)
		if err != nil {
			return failErr(1, err)
		}
		if status.State != secret.MaterialStateEncrypted {
			return failErr(1, secretShowUnavailable(store, name, part, ctx.SecretsDir, status, c.Flags().Changed("part")))
		}
		data, err := store.Read(key)
		if err != nil {
			return failErr(1, err)
		}
		if _, err := stdout.Write(data); err != nil {
			return failErr(1, fmt.Errorf("write secret %s: %w", name, err))
		}
		if !c.Flags().Changed("part") {
			noteOtherSecretParts(stderr, store, name, part)
		}
		return nil
	}
	return cmd
}

func localSecretPartRole(part string) (secret.MaterialRole, error) {
	switch part {
	case "primary":
		return secret.MaterialPrimary, nil
	case "private":
		return secret.MaterialSSHPrivate, nil
	case "public":
		return secret.MaterialSSHPublic, nil
	case "tls-key":
		return secret.MaterialTLSKey, nil
	default:
		return "", fmt.Errorf("--part must be one of primary, private, public, tls-key")
	}
}

var secretShowParts = []struct {
	part string
	role secret.MaterialRole
}{
	{"primary", secret.MaterialPrimary},
	{"private", secret.MaterialSSHPrivate},
	{"public", secret.MaterialSSHPublic},
	{"tls-key", secret.MaterialTLSKey},
}

func presentSecretParts(store *secret.ContextStore, name, exclude string) []string {
	var parts []string
	for _, p := range secretShowParts {
		if p.part == exclude {
			continue
		}
		status, err := store.Inspect(secret.MaterialKey{Name: name, Role: p.role})
		if err == nil && status.State == secret.MaterialStateEncrypted {
			parts = append(parts, p.part)
		}
	}
	return parts
}

func secretShowUnavailable(store *secret.ContextStore, name, part, secretsDir string, status secret.MaterialStatus, partSpecified bool) error {
	present := presentSecretParts(store, name, part)
	var b strings.Builder
	switch {
	case len(present) > 0 && !partSpecified:
		b.WriteString(fmt.Sprintf("secret %q needs an explicit --part; it provides: %s", name, strings.Join(present, ", ")))
	case len(present) > 0:
		b.WriteString(fmt.Sprintf("secret %q has no %s material; it provides: %s", name, part, strings.Join(present, ", ")))
	default:
		b.WriteString(fmt.Sprintf("secret %q not found in %s", name, secretsDir))
	}
	if status.Message != "" && (status.State == secret.MaterialStateUnsafe || len(present) == 0) {
		b.WriteString(fmt.Sprintf("\n%s: %s", status.State, status.Message))
	}
	switch {
	case len(present) > 0:
		b.WriteString("\nre-run with " + strings.Join(partFlags(present), " or "))
	case status.State == secret.MaterialStateUnsafe:
		b.WriteString("\nre-run with sudo if the secret store is root-owned")
	}
	return errors.New(b.String())
}

func noteOtherSecretParts(stderr io.Writer, store *secret.ContextStore, name, shown string) {
	other := presentSecretParts(store, name, shown)
	if len(other) == 0 {
		return
	}
	detail := fmt.Sprintf("showed %s; also available: %s", shown, strings.Join(partFlags(other), ", "))
	output.New(stderr).Status(output.StatusInfo, "secret "+name, detail)
}

func partFlags(parts []string) []string {
	flags := make([]string, 0, len(parts))
	for _, p := range parts {
		flags = append(flags, "--part "+p)
	}
	return flags
}
