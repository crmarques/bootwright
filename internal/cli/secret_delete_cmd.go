package cli

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newSecretDeleteCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var name string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete --name <name>",
		Short: "Delete local secret material from the current context",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&name, "name", "", "SecretRef name (required)")
	addYesFlag(cmd, &yes, "delete")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if !desiredstate.IsDNSLabel(name) {
			return failf(2, "--name must be a lowercase DNS label")
		}
		ctx, err := cf.resolveLocalOnly()
		if err != nil {
			return failErr(1, err)
		}
		output.New(stdout).Command("secret delete")
		materials, err := existingSecretMaterials(ctx.Name, ctx.SecretsDir, name)
		if err != nil {
			return failErr(1, err)
		}
		if len(materials) == 0 {
			return failf(1, "secret %q not found in %s", name, ctx.SecretsDir)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete secret %s from %s? [y/N] (default: no): ", name, ctx.SecretsDir)) {
			return failErr(1, errors.New("secret delete aborted"))
		}
		store := secret.NewContextStore(ctx.Name, ctx.SecretsDir)
		var paths []string
		for _, material := range materials {
			if err := store.Delete(material.Key); err != nil {
				return failErr(1, err)
			}
			paths = append(paths, material.Path)
		}
		output.NewContinuation(stdout).Summary(output.StatusOK, name, "deleted "+strings.Join(paths, ", "))
		return nil
	}
	return cmd
}

func existingSecretMaterials(contextName, secretsDir, name string) ([]secret.MaterialStatus, error) {
	store := secret.NewContextStore(contextName, secretsDir)
	var out []secret.MaterialStatus
	primary, err := store.Inspect(secret.MaterialKey{Name: name, Role: secret.MaterialPrimary})
	if err != nil {
		return nil, err
	}
	private, err := store.Inspect(secret.MaterialKey{Name: name, Role: secret.MaterialSSHPrivate})
	if err != nil {
		return nil, err
	}
	switch {
	case primary.State == secret.MaterialStateEncrypted:
		out = append(out, primary)
	case private.State == secret.MaterialStateEncrypted:
		out = append(out, private)
	case primary.State != secret.MaterialStateMissing:
		return nil, fmt.Errorf("refusing to delete plaintext or unsafe context secret %s: run bootwright secret encryption migrate first", filepath.Join(secretsDir, name))
	}
	for _, role := range []secret.MaterialRole{secret.MaterialTLSKey, secret.MaterialSSHPublic} {
		status, err := store.Inspect(secret.MaterialKey{Name: name, Role: role})
		if err != nil {
			return nil, err
		}
		switch status.State {
		case secret.MaterialStateEncrypted:
			out = append(out, status)
		case secret.MaterialStateMissing:
		default:
			return nil, fmt.Errorf("refusing to delete plaintext or unsafe context secret %s: run bootwright secret encryption migrate first", status.Path)
		}
	}
	return out, nil
}
