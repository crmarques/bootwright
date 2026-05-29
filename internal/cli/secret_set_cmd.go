package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/runtime/fs"
	"github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newSecretSetCmd(stdout io.Writer) *cobra.Command {
	var (
		pullSecret    string
		fromFile      string
		tlsCert       string
		tlsKey        string
		username      string
		password      string
		passwordStdin bool
		generate      bool
	)
	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Store a pull secret or username:password credentials for a SecretRef",
		Long: `Write the named SecretRef material to the current context secrets-dir, mode 0600.
Exactly one input mode is required:

  --pull-secret <file>             store an OpenShift pull-secret JSON file
  --tls-cert <file> --tls-key <file> store a TLS certificate chain and key
  --from-file <file>               store an existing "username:password" file
  --username <u> --password-stdin  read the password from stdin
  --generate                       generate a random password (default username "admin")

Prefer --password-stdin or --from-file for credentials the operator
provides. Use --generate for test fixtures.`,
		Args: cobra.ExactArgs(1),
		Example: `  # Store an OpenShift pull secret
  bootwright secret set openshift-pull-secret --pull-secret ~/pull-secret.json

  # Store BMC / proxy credentials interactively
  bootwright secret set proxy-credentials --username proxy --password-stdin

  # Use a pre-existing username:password file
  bootwright secret set bmc-credentials --from-file ./bmc.txt

  # Generate a random password for a test fixture
  bootwright secret set proxy-credentials --generate --username proxy`,
	}
	cmd.Flags().StringVar(&pullSecret, "pull-secret", "", "path to an OpenShift pull-secret JSON file")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "path to a PEM TLS certificate chain file")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "path to a PEM TLS private key file")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to a file containing one line: username:password")
	cmd.Flags().StringVar(&username, "username", "", "username (required with --password, --password-stdin, or --generate)")
	cmd.Flags().StringVar(&password, "password", "", "password (mutually exclusive with --password-stdin and --generate)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read password from stdin instead of --password")
	cmd.Flags().BoolVar(&generate, "generate", false, "generate a strong random password (intended for test fixtures)")
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, args []string) error {
		name := args[0]
		if !desiredstate.IsDNSLabel(name) {
			return failf(2, "<name> must be a lowercase DNS label")
		}
		modes := 0
		if pullSecret != "" {
			modes++
		}
		if tlsCert != "" || tlsKey != "" {
			if tlsCert == "" || tlsKey == "" {
				return failf(2, "--tls-cert and --tls-key must be set together")
			}
			modes++
		}
		if fromFile != "" {
			modes++
		}
		if password != "" {
			modes++
		}
		if passwordStdin {
			modes++
		}
		if generate {
			modes++
		}
		if modes == 0 {
			return failf(2, "one of --pull-secret, --tls-cert/--tls-key, --from-file, --password, --password-stdin, or --generate is required")
		}
		if modes > 1 {
			return failf(2, "--pull-secret, --tls-cert/--tls-key, --from-file, --password, --password-stdin, and --generate are mutually exclusive")
		}
		if shouldRunLocalRootChild() {
			code, err := runSecretSetWithLocalRoot(c.Context(), c.InOrStdin(), stdout, c.ErrOrStderr(), name, pullSecret, tlsCert, tlsKey, fromFile, username, password, passwordStdin, generate)
			if err != nil {
				return failErr(1, err)
			}
			if code != 0 {
				return silentExit(code)
			}
			return nil
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		warnSecretsDirPerms(ctx.SecretsDir, c.ErrOrStderr())
		if pullSecret != "" {
			return runSecretSetPullSecret(stdout, name, pullSecret, ctx.SecretsDir)
		}
		if tlsCert != "" {
			return runSecretSetTLS(stdout, name, tlsCert, tlsKey, ctx.SecretsDir)
		}
		return runSecretSetCredentials(c, stdout, name, fromFile, username, password, passwordStdin, generate, ctx.SecretsDir)
	}
	return cmd
}

func runSecretSetTLS(stdout io.Writer, name, certFile, keyFile, secretsDir string) error {
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return failErr(1, fmt.Errorf("read TLS certificate file %s: %w", certFile, err))
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return failErr(1, fmt.Errorf("read TLS private key file %s: %w", keyFile, err))
	}
	if _, err := secret.ValidateTLSCertificateKey(certData, keyData); err != nil {
		return failErr(1, err)
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return failErr(1, fmt.Errorf("create secrets directory %s: %w", secretsDir, err))
	}
	if err := os.Chmod(secretsDir, 0o700); err != nil {
		return failErr(1, fmt.Errorf("chmod secrets directory %s: %w", secretsDir, err))
	}
	certTarget := filepath.Join(secretsDir, name)
	keyTarget := certTarget + ".key"
	certExists, err := safefs.RegularFileExists(certTarget)
	if err != nil {
		return failErr(1, err)
	}
	keyExists, err := safefs.RegularFileExists(keyTarget)
	if err != nil {
		return failErr(1, err)
	}
	if err := safefs.AtomicWriteFile(certTarget, certData, 0o600); err != nil {
		return failErr(1, err)
	}
	if err := safefs.AtomicWriteFile(keyTarget, keyData, 0o600); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if certExists || keyExists {
		action = "updated"
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, fmt.Sprintf("%s TLS certificate and key at %s and %s", action, certTarget, keyTarget))
	return nil
}

func runSecretSetPullSecret(stdout io.Writer, name, fromFile, secretsDir string) error {
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return failErr(1, fmt.Errorf("read pull secret file %s: %w", fromFile, err))
	}
	if err := secret.ValidatePullSecretJSON(data); err != nil {
		return failErr(1, err)
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return failErr(1, fmt.Errorf("create secrets directory %s: %w", secretsDir, err))
	}
	if err := os.Chmod(secretsDir, 0o700); err != nil {
		return failErr(1, fmt.Errorf("chmod secrets directory %s: %w", secretsDir, err))
	}
	target := filepath.Join(secretsDir, name)
	exists, err := safefs.RegularFileExists(target)
	if err != nil {
		return failErr(1, err)
	}
	if err := safefs.AtomicWriteFile(target, data, 0o600); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if exists {
		action = "updated"
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, fmt.Sprintf("%s pull secret at %s", action, target))
	return nil
}

func runSecretSetCredentials(c *cobra.Command, stdout io.Writer, name, fromFile, username, password string, passwordStdin, generate bool, secretsDir string) error {
	var resolvedUser, resolvedPass string
	switch {
	case fromFile != "":
		data, err := os.ReadFile(fromFile)
		if err != nil {
			return failErr(1, fmt.Errorf("read credentials file %s: %w", fromFile, err))
		}
		u, p, err := secret.ParseBMCCredentials(data)
		if err != nil {
			return failErr(1, err)
		}
		resolvedUser, resolvedPass = u, p
	case password != "":
		if username == "" {
			return failf(2, "--username is required with --password")
		}
		resolvedUser, resolvedPass = username, password
	case passwordStdin:
		if username == "" {
			return failf(2, "--username is required with --password-stdin")
		}
		stdin := c.InOrStdin()
		if stdin == nil {
			return failErr(1, errors.New("--password-stdin requires stdin"))
		}
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			return failErr(1, fmt.Errorf("read password from stdin: %w", err))
		}
		resolvedUser, resolvedPass = username, strings.TrimRight(line, "\r\n")
	case generate:
		resolvedUser = username
		if resolvedUser == "" {
			resolvedUser = "admin"
		}
		generated, err := secret.GenerateBMCPassword()
		if err != nil {
			return failErr(1, err)
		}
		resolvedPass = generated
	}
	if err := secret.ValidateBMCUsername(resolvedUser); err != nil {
		return failErr(1, err)
	}
	if resolvedPass == "" {
		return failErr(1, errors.New("password must not be empty"))
	}
	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		return failErr(1, fmt.Errorf("create secrets directory %s: %w", secretsDir, err))
	}
	if err := os.Chmod(secretsDir, 0o700); err != nil {
		return failErr(1, fmt.Errorf("chmod secrets directory %s: %w", secretsDir, err))
	}
	target := filepath.Join(secretsDir, name)
	exists, err := safefs.RegularFileExists(target)
	if err != nil {
		return failErr(1, err)
	}
	payload := []byte(resolvedUser + ":" + resolvedPass + "\n")
	if err := safefs.AtomicWriteFile(target, payload, 0o600); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if exists {
		action = "updated"
	}
	message := fmt.Sprintf("%s credentials at %s (user %q)", action, target, resolvedUser)
	if generate {
		message += " — password generated; copy it from the file above before sharing"
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, message)
	return nil
}
