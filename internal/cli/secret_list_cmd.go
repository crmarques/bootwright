package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/status"
)

type secretListReport struct {
	Context string            `json:"context"`
	Secrets []secretListEntry `json:"secrets"`
}

type secretListEntry = status.SecretEntry

func newSecretListCmd(stdout io.Writer) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List declared secrets and local material status",
		Args:  cobra.NoArgs,
	}
	addOutputFlag(cmd, &outputFormat)
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		entries, err := declaredSecretEntriesForContext(cf.ctx.Name, cf.ctx.SecretsDir, state)
		if err != nil {
			return failErr(1, err)
		}
		if outputFormat == outputJSON {
			return output.JSON(stdout, secretListReport{Context: cf.ctx.Name, Secrets: entries})
		}
		p := output.New(stdout)
		p.Command("secret list")
		if len(entries) == 0 {
			p.Summary(output.StatusSkip, "secrets", "none declared")
			return nil
		}
		var checks []output.Check
		for _, entry := range entries {
			status := output.StatusOK
			if !entry.Present {
				status = output.StatusFail
			}
			evidence := entry.Type + " " + strings.Join(entry.Paths, ", ")
			if entry.Detail != "" {
				evidence += " (" + entry.Detail + ")"
			}
			checks = append(checks, output.Check{
				Group:    "Declared secrets",
				Name:     entry.Name,
				Status:   status,
				Evidence: evidence,
			})
		}
		p.Checks(checks)
		return nil
	}
	return cmd
}

func declaredSecretEntries(secretsDir string, state v1alpha1.State) ([]secretListEntry, error) {
	return declaredSecretEntriesForContext("test", secretsDir, state)
}

func declaredSecretEntriesForContext(contextName, secretsDir string, state v1alpha1.State) ([]secretListEntry, error) {
	env := stateview.Environment(state)
	if env == nil || len(env.Spec.Secrets) == 0 {
		return nil, nil
	}
	store := secret.NewContextStore(contextName, secretsDir)
	names := make([]string, 0, len(env.Spec.Secrets))
	for name := range env.Spec.Secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]secretListEntry, 0, len(names))
	for _, name := range names {
		spec := env.Spec.Secrets[name]
		typ := secretSpecType(name, spec, state)
		pathEntries := secretSpecPathEntries(name, spec, env, secretsDir, state)
		paths := secretPathEntryPaths(pathEntries)
		present, detail := secretPathsPresent(store, pathEntries)
		entries = append(entries, secretListEntry{
			Name:    name,
			Type:    typ,
			Paths:   paths,
			Present: present,
			Detail:  detail,
		})
	}
	return entries, nil
}

type secretPathEntry struct {
	name           string
	role           secret.MaterialRole
	path           string
	externalSource bool
}

func secretSpecType(name string, spec v1alpha1.EnvironmentSecretSpec, state v1alpha1.State) string {
	switch {
	case secret.ConsumedAsTLS(name, state):
		if spec.Generated != nil && spec.Generated.SelfSignedCertificate != nil {
			return "generated:selfSignedCertificate"
		}
		if spec.File != "" {
			return "file:tls"
		}
		return "context:tls"
	case spec.Generated != nil && spec.Generated.SSHKeyPair != nil:
		return "generated:sshKeyPair"
	case spec.File != "":
		return "file"
	case spec.Generated != nil && spec.Generated.Credentials != nil:
		return "generated:credentials"
	case spec.Generated != nil && spec.Generated.SelfSignedCertificate != nil:
		return "generated:selfSignedCertificate"
	default:
		return "context"
	}
}

func secretSpecPathEntries(name string, spec v1alpha1.EnvironmentSecretSpec, env *v1alpha1.Environment, secretsDir string, state v1alpha1.State) []secretPathEntry {
	entry := func(role secret.MaterialRole) secretPathEntry {
		return secretPathEntry{
			name:           name,
			role:           role,
			path:           secret.ResolveMaterialPath(name, env, secretsDir, role),
			externalSource: secret.MaterialPathUsesExternalSource(name, env, role),
		}
	}
	if spec.Generated != nil && spec.Generated.SSHKeyPair != nil {
		return []secretPathEntry{
			entry(secret.MaterialSSHPrivate),
			entry(secret.MaterialSSHPublic),
		}
	}
	if spec.Generated != nil && spec.Generated.SelfSignedCertificate != nil || secret.ConsumedAsTLS(name, state) {
		return []secretPathEntry{
			entry(secret.MaterialPrimary),
			entry(secret.MaterialTLSKey),
		}
	}
	if env != nil && env.Spec.SecretStorage.Mode == v1alpha1.SecretStorageModeContext && (secret.ConsumedAsClusterSSH(name, state) || secret.ConsumedAsStorageSSH(name, state) || secret.ConsumedAsHostSSH(name, state)) {
		var paths []secretPathEntry
		if secret.ConsumedAsClusterSSHPrivate(name, state) || secret.ConsumedAsStorageSSHPrivate(name, state) || secret.ConsumedAsHostSSH(name, state) {
			paths = append(paths, entry(secret.MaterialSSHPrivate))
		}
		if secret.ConsumedAsClusterSSHPublic(name, state) || secret.ConsumedAsStorageSSHPublic(name, state) {
			paths = append(paths, entry(secret.MaterialSSHPublic))
		}
		return paths
	}
	return []secretPathEntry{entry(secret.MaterialPrimary)}
}

func secretPathEntryPaths(entries []secretPathEntry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.path)
	}
	return paths
}

func secretPathsPresent(store *secret.ContextStore, paths []secretPathEntry) (bool, string) {
	for _, path := range paths {
		if path.externalSource {
			info, err := secret.StatExternalFile(path.path)
			if errors.Is(err, os.ErrNotExist) {
				return false, "missing " + path.path
			}
			if err != nil {
				return false, fmt.Sprintf("stat %s: %v", path.path, err)
			}
			if info.IsDir() {
				return false, path.path + " is a directory"
			}
			continue
		}
		status, err := store.Inspect(secret.MaterialKey{Name: path.name, Role: path.role})
		if err != nil {
			return false, err.Error()
		}
		if status.State != secret.MaterialStateEncrypted {
			if status.Message != "" {
				return false, fmt.Sprintf("%s %s: %s", path.path, status.State, status.Message)
			}
			return false, fmt.Sprintf("%s %s", path.path, status.State)
		}
	}
	return true, ""
}
