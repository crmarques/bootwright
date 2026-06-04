package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
)

func newSecretEncryptionCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "encryption",
		Short: "Manage context-local secret encryption",
	}
	cmd.AddCommand(
		newSecretEncryptionInitCmd(stdout),
		newSecretEncryptionStatusCmd(stdout),
		newSecretEncryptionMigrateCmd(stdin, stdout),
		newSecretEncryptionRotateCmd(stdin, stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newSecretEncryptionInitCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the current context encrypted secret store",
		Args:  cobra.NoArgs,
	}
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		store := secret.NewContextStore(ctx.Name, ctx.SecretsDir)
		if err := store.EnsureInitialized(); err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("secret encryption init")
		p.Summary(output.StatusOK, "encryption", "initialized root-owned-file keyring under "+ctx.SecretsDir)
		return nil
	}
	return cmd
}

func newSecretEncryptionStatusCmd(stdout io.Writer) *cobra.Command {
	outputFormat := outputText
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show context-local secret encryption status",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&outputFormat, "output", outputFormat, "output format: text|json")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		status := secret.NewContextStore(ctx.Name, ctx.SecretsDir).Status()
		if outputFormat == outputJSON {
			return output.JSON(stdout, status)
		}
		p := output.New(stdout)
		p.Command("secret encryption status")
		if !status.Initialized {
			p.Summary(output.StatusWarn, "encryption", statusMessage(status, "not initialized"))
			return nil
		}
		p.Status(output.StatusOK, "provider", status.KeyProvider)
		p.Status(output.StatusOK, "active key", status.ActiveKeyID)
		p.Status(output.StatusOK, "encrypted material", fmt.Sprintf("%d", status.Encrypted))
		if status.Plaintext > 0 || status.Unsafe > 0 || len(status.ValidationErrs) > 0 {
			for _, err := range status.ValidationErrs {
				p.Status(output.StatusFail, "validation", err)
			}
			for _, material := range status.Material {
				if material.State == secret.MaterialStateEncrypted || material.State == secret.MaterialStateMissing {
					continue
				}
				p.Status(output.StatusFail, material.Key.Name, materialStateEvidence(material))
			}
			p.Summary(output.StatusFail, "encryption", "store has blocked plaintext or unsafe entries")
			return nil
		}
		p.Summary(output.StatusOK, "encryption", "store is encrypted")
		return nil
	}
	return cmd
}

func newSecretEncryptionMigrateCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Encrypt existing context-local plaintext secret files",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the migration confirmation prompt")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Encrypt plaintext secrets in %s? [y/N] (default: no): ", ctx.SecretsDir)) {
			return failErr(1, errors.New("secret encryption migrate aborted"))
		}
		store := secret.NewContextStore(ctx.Name, ctx.SecretsDir)
		migrated, err := store.MigratePlaintext(migrationPrimaryRoleForState(state))
		if err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("secret encryption migrate")
		for _, material := range migrated {
			p.Status(output.StatusOK, material.Key.Name, fmt.Sprintf("%s %s", material.Key.Role, material.Path))
		}
		p.Summary(output.StatusOK, "encryption", fmt.Sprintf("migrated %d material file(s)", len(migrated)))
		return nil
	}
	return cmd
}

func newSecretEncryptionRotateCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate the active context-local secret encryption key",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the rotation confirmation prompt")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Rotate encryption keys for %s? [y/N] (default: no): ", ctx.SecretsDir)) {
			return failErr(1, errors.New("secret encryption rotate aborted"))
		}
		store := secret.NewContextStore(ctx.Name, ctx.SecretsDir)
		if err := store.Rotate(); err != nil {
			return failErr(1, err)
		}
		status := store.Status()
		p := output.New(stdout)
		p.Command("secret encryption rotate")
		p.Summary(output.StatusOK, "encryption", "rotated active key to "+status.ActiveKeyID)
		return nil
	}
	return cmd
}

func migrationPrimaryRoleForState(state v1alpha1.State) func(string) secret.MaterialRole {
	return func(name string) secret.MaterialRole {
		if secretConsumedAsTLS(name, state) {
			return secret.MaterialPrimary
		}
		env := primaryEnvironmentForSync(state)
		if env != nil {
			spec, ok := env.Spec.Secrets[name]
			if ok && spec.Generated != nil {
				if spec.Generated.SSHKeyPair != nil {
					return secret.MaterialSSHPrivate
				}
				return secret.MaterialPrimary
			}
		}
		if secretConsumedAsClusterSSHPrivate(name, state) || secretConsumedAsStorageSSHPrivate(name, state) || secretConsumedAsHostSSH(name, state) {
			return secret.MaterialSSHPrivate
		}
		return secret.MaterialPrimary
	}
}

func statusMessage(status secret.StoreStatus, fallback string) string {
	if status.Message != "" {
		return status.Message
	}
	return fallback
}

func materialStateEvidence(material secret.MaterialStatus) string {
	parts := []string{string(material.State), material.Path}
	if material.Message != "" {
		parts = append(parts, material.Message)
	}
	return strings.Join(parts, " ")
}
