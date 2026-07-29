package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/state/desired"
)

func newSecretSetCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var (
		name          string
		pullSecret    string
		rawFile       string
		fromFile      string
		tlsCert       string
		tlsKey        string
		username      string
		passwordStdin bool
		generate      bool
		yes           bool
	)
	cmd := &cobra.Command{
		Use:   "set --name <name>",
		Short: "Store a pull secret or username:password credentials for a SecretRef",
		Long: `Write the named SecretRef material to the current context secrets-dir, mode 0600.
Exactly one input mode is required:

  --pull-secret <file>             store an OpenShift pull-secret JSON file
  --tls-cert <file> --tls-key <file> store a TLS certificate chain and key
  --raw-file <file>                store arbitrary secret bytes
  --from-file <file>               store an existing "username:password" file
  --username <u> --password-stdin  read the password from stdin
  --generate                       generate a random password (default username "admin")

Prefer --password-stdin or --from-file for credentials the operator
provides. Use --generate for test fixtures.`,
		Args: cobra.NoArgs,
		Example: `  # Store an OpenShift pull secret
  bootwright secret set --name openshift-pull-secret --pull-secret ~/pull-secret.json

  # Store BMC / proxy credentials interactively
  bootwright secret set --name proxy-credentials --username proxy --password-stdin

  # Use a pre-existing username:password file
  bootwright secret set --name bmc-credentials --from-file ./bmc.txt

  # Store imported Data Foundation external cluster details JSON
  bootwright secret set --name shared-ceph-external-details --raw-file ./external-details.json

  # Generate a random password for a test fixture
  bootwright secret set --name proxy-credentials --generate --username proxy`,
	}
	cmd.Flags().StringVar(&name, "name", "", "SecretRef name (required)")
	cmd.Flags().StringVar(&pullSecret, "pull-secret", "", "path to an OpenShift pull-secret JSON file")
	cmd.Flags().StringVar(&tlsCert, "tls-cert", "", "path to a PEM TLS certificate chain file")
	cmd.Flags().StringVar(&tlsKey, "tls-key", "", "path to a PEM TLS private key file")
	cmd.Flags().StringVar(&rawFile, "raw-file", "", "path to a file containing arbitrary secret bytes")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "path to a file containing one line: username:password")
	cmd.Flags().StringVar(&username, "username", "", "username (required with --password-stdin; defaults to admin with --generate)")
	cmd.Flags().BoolVar(&passwordStdin, "password-stdin", false, "read the password from stdin")
	cmd.Flags().BoolVar(&generate, "generate", false, "generate a strong random password (intended for test fixtures)")
	addYesFlag(cmd, &yes, "overwrite")
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if !desiredstate.IsDNSLabel(name) {
			return failf(2, "--name must be a lowercase DNS label")
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
		if rawFile != "" {
			modes++
		}
		if passwordStdin {
			modes++
		}
		if generate {
			modes++
		}
		if modes == 0 {
			return failf(2, "one of --pull-secret, --tls-cert/--tls-key, --raw-file, --from-file, --password-stdin, or --generate is required")
		}
		if modes > 1 {
			return failf(2, "--pull-secret, --tls-cert/--tls-key, --raw-file, --from-file, --password-stdin, and --generate are mutually exclusive")
		}
		if shouldRunLocalRootChild() {
			code, err := runSecretSetWithLocalRoot(c.Context(), c.InOrStdin(), stdout, c.ErrOrStderr(), name, pullSecret, tlsCert, tlsKey, rawFile, fromFile, username, passwordStdin, generate, yes)
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
			return runSecretSetPullSecret(stdin, stdout, name, pullSecret, ctx.Name, ctx.SecretsDir, yes)
		}
		if tlsCert != "" {
			return runSecretSetTLS(stdin, stdout, name, tlsCert, tlsKey, ctx.Name, ctx.SecretsDir, yes)
		}
		if rawFile != "" {
			return runSecretSetRawFile(stdin, stdout, name, rawFile, ctx.Name, ctx.SecretsDir, yes)
		}
		return runSecretSetCredentials(c, stdin, stdout, name, fromFile, username, passwordStdin, generate, ctx.Name, ctx.SecretsDir, yes)
	}
	return cmd
}

func runSecretSetTLS(stdin io.Reader, stdout io.Writer, name, certFile, keyFile, contextName, secretsDir string, yes bool) error {
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
	store := secret.NewContextStore(contextName, secretsDir)
	certKey := secret.MaterialKey{Name: name, Role: secret.MaterialPrimary}
	tlsKey := secret.MaterialKey{Name: name, Role: secret.MaterialTLSKey}
	certExists, err := store.Exists(certKey)
	if err != nil {
		return failErr(1, err)
	}
	keyExists, err := store.Exists(tlsKey)
	if err != nil {
		return failErr(1, err)
	}
	if err := confirmSecretSetOverwrite(stdin, stdout, name, secretsDir, certExists || keyExists, yes); err != nil {
		return err
	}
	if err := store.Write(certKey, certData); err != nil {
		return failErr(1, err)
	}
	if err := store.Write(tlsKey, keyData); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if certExists || keyExists {
		action = "updated"
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, fmt.Sprintf("%s encrypted TLS certificate and key under %s", action, secretsDir))
	printNextStatusHint(stdout)
	return nil
}

func runSecretSetPullSecret(stdin io.Reader, stdout io.Writer, name, fromFile, contextName, secretsDir string, yes bool) error {
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return failErr(1, fmt.Errorf("read pull secret file %s: %w", fromFile, err))
	}
	if err := secret.ValidatePullSecretJSON(data); err != nil {
		return failErr(1, err)
	}
	store := secret.NewContextStore(contextName, secretsDir)
	key := secret.MaterialKey{Name: name, Role: secret.MaterialPrimary}
	exists, err := store.Exists(key)
	if err != nil {
		return failErr(1, err)
	}
	if err := confirmSecretSetOverwrite(stdin, stdout, name, secretsDir, exists, yes); err != nil {
		return err
	}
	if err := store.Write(key, data); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if exists {
		action = "updated"
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, fmt.Sprintf("%s encrypted pull secret under %s", action, secretsDir))
	printNextStatusHint(stdout)
	return nil
}

func runSecretSetRawFile(stdin io.Reader, stdout io.Writer, name, fromFile, contextName, secretsDir string, yes bool) error {
	data, err := os.ReadFile(fromFile)
	if err != nil {
		return failErr(1, fmt.Errorf("read raw secret file %s: %w", fromFile, err))
	}
	store := secret.NewContextStore(contextName, secretsDir)
	key := secret.MaterialKey{Name: name, Role: secret.MaterialPrimary}
	exists, err := store.Exists(key)
	if err != nil {
		return failErr(1, err)
	}
	if err := confirmSecretSetOverwrite(stdin, stdout, name, secretsDir, exists, yes); err != nil {
		return err
	}
	if err := store.Write(key, data); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if exists {
		action = "updated"
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, fmt.Sprintf("%s encrypted raw secret under %s", action, secretsDir))
	printNextStatusHint(stdout)
	return nil
}

func runSecretSetCredentials(c *cobra.Command, stdin io.Reader, stdout io.Writer, name, fromFile, username string, passwordStdin, generate bool, contextName, secretsDir string, yes bool) error {
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
	store := secret.NewContextStore(contextName, secretsDir)
	key := secret.MaterialKey{Name: name, Role: secret.MaterialPrimary}
	exists, err := store.Exists(key)
	if err != nil {
		return failErr(1, err)
	}
	if err := confirmSecretSetOverwrite(stdin, stdout, name, secretsDir, exists, yes); err != nil {
		return err
	}
	payload := []byte(resolvedUser + ":" + resolvedPass + "\n")
	if err := store.Write(key, payload); err != nil {
		return failErr(1, err)
	}
	action := "wrote"
	if exists {
		action = "updated"
	}
	message := fmt.Sprintf("%s encrypted credentials under %s (user %q)", action, secretsDir, resolvedUser)
	if generate {
		message += fmt.Sprintf(" — password generated; reveal it with `bootwright secret show --name %s`", name)
	}
	p := output.New(stdout)
	p.Command("secret set")
	p.Summary(output.StatusOK, name, message)
	printNextStatusHint(stdout)
	return nil
}

func confirmSecretSetOverwrite(stdin io.Reader, stdout io.Writer, name, secretsDir string, exists bool, yes bool) error {
	if !exists || yes {
		return nil
	}
	if !confirm(stdin, stdout, fmt.Sprintf("Replace secret %s in %s? [y/N] (default: no): ", name, secretsDir)) {
		return failErr(1, errors.New("secret set aborted"))
	}
	return nil
}
