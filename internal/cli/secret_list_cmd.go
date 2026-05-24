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
)

type secretListReport struct {
	Context string            `json:"context"`
	Secrets []secretListEntry `json:"secrets"`
}

type secretListEntry struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Paths   []string `json:"paths"`
	Present bool     `json:"present"`
	Detail  string   `json:"detail,omitempty"`
}

func newSecretListCmd(stdout io.Writer) *cobra.Command {
	outputFormat := outputText
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List declared secrets and local material status",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&outputFormat, "output", outputFormat, "output format: text|json")
	cf := addCommonFlags()
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		entries, err := declaredSecretEntries(cf.ctx.SecretsDir, state)
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
			checks = append(checks, output.Check{
				Group:    "Declared secrets",
				Name:     entry.Name,
				Status:   status,
				Evidence: entry.Type + " " + strings.Join(entry.Paths, ", "),
			})
		}
		p.Checks(checks)
		return nil
	}
	return cmd
}

func declaredSecretEntries(secretsDir string, state v1alpha1.State) ([]secretListEntry, error) {
	env := primaryEnvironmentForSync(state)
	if env == nil || len(env.Spec.Secrets) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(env.Spec.Secrets))
	for name := range env.Spec.Secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]secretListEntry, 0, len(names))
	for _, name := range names {
		spec := env.Spec.Secrets[name]
		typ := secretSpecType(spec)
		paths := secretSpecPaths(name, spec, env, secretsDir)
		present, detail, err := secretPathsPresent(paths)
		if err != nil {
			return nil, err
		}
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

func secretSpecType(spec v1alpha1.EnvironmentSecretSpec) string {
	switch {
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

func secretSpecPaths(name string, spec v1alpha1.EnvironmentSecretSpec, env *v1alpha1.Environment, secretsDir string) []string {
	path := resolvedSecretPath(name, env, secretsDir)
	if spec.Generated != nil && spec.Generated.SelfSignedCertificate != nil {
		return []string{path, path + ".key"}
	}
	return []string{path}
}

func secretPathsPresent(paths []string) (bool, string, error) {
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return false, "missing " + path, nil
		}
		if err != nil {
			return false, "", fmt.Errorf("stat %s: %w", path, err)
		}
		if info.IsDir() {
			return false, path + " is a directory", nil
		}
	}
	return true, "", nil
}
