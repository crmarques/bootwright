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

func declaredSecretEntriesForContext(contextName, secretsDir string, state v1alpha1.State) ([]secretListEntry, error) {
	if len(state.Secrets) == 0 {
		return nil, nil
	}
	store := secret.NewContextStore(contextName, secretsDir)
	secrets := append([]v1alpha1.Secret(nil), state.Secrets...)
	sort.Slice(secrets, func(i, j int) bool { return secrets[i].Metadata.Name < secrets[j].Metadata.Name })
	idx := secret.NewIndex(state)
	entries := make([]secretListEntry, 0, len(secrets))
	for _, s := range secrets {
		pathEntries := secretPathEntriesForSecret(s, idx, secretsDir)
		paths := secretPathEntryPaths(pathEntries)
		present, detail := secretPathsPresent(store, pathEntries)
		entries = append(entries, secretListEntry{
			Name:    s.Metadata.Name,
			Type:    secretTypeLabel(s),
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

// secretTypeLabel is the "<source>:<type>" display label for a declared Secret.
func secretTypeLabel(s v1alpha1.Secret) string {
	source := "context"
	switch {
	case s.Spec.Source.Generated != nil:
		source = "generated"
	case s.Spec.Source.File != nil:
		source = "file"
	}
	return source + ":" + s.Spec.Type
}

// secretPathEntriesForSecret lists the material roles a Secret carries, keyed by
// its type: sshKeyPair carries a private and public half, tlsCertificate a cert
// and key, every other type a single primary material.
func secretPathEntriesForSecret(s v1alpha1.Secret, idx secret.Index, secretsDir string) []secretPathEntry {
	name := s.Metadata.Name
	entry := func(role secret.MaterialRole) secretPathEntry {
		return secretPathEntry{
			name:           name,
			role:           role,
			path:           secret.ResolveMaterialPath(name, idx, secretsDir, role),
			externalSource: secret.MaterialPathUsesExternalSource(name, idx, role),
		}
	}
	switch s.Spec.Type {
	case v1alpha1.SecretTypeSSHKeyPair:
		return []secretPathEntry{entry(secret.MaterialSSHPrivate), entry(secret.MaterialSSHPublic)}
	case v1alpha1.SecretTypeTLSCertificate:
		return []secretPathEntry{entry(secret.MaterialPrimary), entry(secret.MaterialTLSKey)}
	default:
		return []secretPathEntry{entry(secret.MaterialPrimary)}
	}
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
