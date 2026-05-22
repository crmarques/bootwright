package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/safefs"
	"github.com/crmarques/bootwright/internal/secret"
)

func newSecretGenerateCmd(stdout io.Writer, _ io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate local install secret material requested by desired state",
		Args:  cobra.NoArgs,
		Example: `  # Materialize every "generated:" secret declared by the current context
  bootwright secret generate`,
	}
	cf := addCommonFlags(cmd)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		certRequests, err := generatedSelfSignedRequests(state)
		if err != nil {
			return failErr(1, err)
		}
		credRequests := generatedCredentialsRequestsFor(state)
		p := output.New(stdout)
		p.Command("secret generate")
		if len(certRequests) == 0 && len(credRequests) == 0 {
			p.Summary(output.StatusSkip, "secret generate", "no generated secret requests found")
			return nil
		}
		if err := os.MkdirAll(ctx.SecretsDir, 0o700); err != nil {
			return failErr(1, fmt.Errorf("create secrets directory %s: %w", ctx.SecretsDir, err))
		}
		if err := os.Chmod(ctx.SecretsDir, 0o700); err != nil {
			return failErr(1, fmt.Errorf("chmod secrets directory %s: %w", ctx.SecretsDir, err))
		}
		for _, request := range certRequests {
			action, err := materializeSelfSignedCertificate(ctx.SecretsDir, request)
			if err != nil {
				return failErr(1, err)
			}
			p.Status(output.StatusOK, request.name, action)
		}
		for _, request := range credRequests {
			action, err := materializeGeneratedCredentials(ctx.SecretsDir, request)
			if err != nil {
				return failErr(1, err)
			}
			p.Status(output.StatusOK, request.name, action)
		}
		p.Summary(output.StatusOK, "secret generate", fmt.Sprintf("%d secret request(s) handled", len(certRequests)+len(credRequests)))
		return nil
	}
	return cmd
}

func materializeGeneratedCredentials(secretsDir string, request generatedCredentialsRequest) (string, error) {
	target := filepath.Join(secretsDir, request.name)
	wantUser := request.credentials.Username
	if wantUser == "" {
		wantUser = "admin"
	}
	exists, err := safefs.RegularFileExists(target)
	if err != nil {
		return "", err
	}
	if exists {
		data, err := os.ReadFile(target)
		if err != nil {
			return "", fmt.Errorf("read existing credentials %s: %w", target, err)
		}
		gotUser, _, perr := secret.ParseBMCCredentials(data)
		if perr != nil {
			return "", fmt.Errorf("existing credentials %s: %w; remove the file to regenerate", target, perr)
		}
		if gotUser != wantUser {
			return "", fmt.Errorf("existing credentials %q at %s use username %q but desired spec wants %q; remove %s to regenerate", request.name, target, gotUser, wantUser, target)
		}
		return "reused existing credentials", nil
	}
	password, err := secret.GenerateBMCPassword()
	if err != nil {
		return "", err
	}
	payload := []byte(wantUser + ":" + password + "\n")
	if err := safefs.AtomicWriteFile(target, payload, 0o600); err != nil {
		return "", err
	}
	return fmt.Sprintf("generated %s (user %q)", target, wantUser), nil
}
