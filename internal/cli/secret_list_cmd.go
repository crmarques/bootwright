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
	"github.com/crmarques/bootwright/internal/runtime/secrets"
	"github.com/crmarques/bootwright/internal/storage/topology"
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
		typ := secretSpecType(name, spec, state)
		pathEntries := secretSpecPathEntries(name, spec, env, secretsDir, state)
		paths := secretPathEntryPaths(pathEntries)
		present, detail := secretPathsPresent(pathEntries)
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
	path           string
	externalSource bool
}

func secretSpecType(name string, spec v1alpha1.EnvironmentSecretSpec, state v1alpha1.State) string {
	switch {
	case secretConsumedAsTLS(name, state):
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
			path:           secret.ResolveMaterialPath(name, env, secretsDir, role),
			externalSource: secret.MaterialPathUsesExternalSource(name, env, role),
		}
	}
	if spec.Generated != nil && spec.Generated.SSHKeyPair != nil {
		return []secretPathEntry{
			entry(secret.MaterialPrimary),
			entry(secret.MaterialSSHPublic),
		}
	}
	if spec.Generated != nil && spec.Generated.SelfSignedCertificate != nil || secretConsumedAsTLS(name, state) {
		return []secretPathEntry{
			entry(secret.MaterialPrimary),
			entry(secret.MaterialTLSKey),
		}
	}
	if env != nil && env.Spec.SecretStorage.Mode == v1alpha1.SecretStorageModeContext && (secretConsumedAsClusterSSH(name, state) || secretConsumedAsStorageSSH(name, state) || secretConsumedAsHostSSH(name, state)) {
		var paths []secretPathEntry
		if secretConsumedAsClusterSSHPrivate(name, state) || secretConsumedAsStorageSSHPrivate(name, state) || secretConsumedAsHostSSH(name, state) {
			paths = append(paths, entry(secret.MaterialSSHPrivate))
		}
		if secretConsumedAsClusterSSHPublic(name, state) || secretConsumedAsStorageSSHPublic(name, state) {
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

func secretConsumedAsTLS(name string, state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		serving := cluster.Spec.Install.ServingCertificates
		if serving == nil {
			continue
		}
		if api := serving.APIServer; api != nil {
			for _, cert := range api.NamedCertificates {
				if cert.SecretRef.Name == name {
					return true
				}
			}
		}
		if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name == name {
			return true
		}
	}
	return false
}

func secretConsumedAsClusterSSH(name string, state v1alpha1.State) bool {
	return secretConsumedAsClusterSSHPublic(name, state) || secretConsumedAsClusterSSHPrivate(name, state)
}

func secretConsumedAsClusterSSHPublic(name string, state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		ssh := cluster.Spec.Install.NodeSSH
		if ssh.KeyPairRef.Name == name || ssh.PublicKeyRef.Name == name {
			return true
		}
	}
	return false
}

func secretConsumedAsClusterSSHPrivate(name string, state v1alpha1.State) bool {
	for _, cluster := range state.ContainerClusters {
		ssh := cluster.Spec.Install.NodeSSH
		if ssh.KeyPairRef.Name == name || ssh.PrivateKeyRef.Name == name {
			return true
		}
	}
	return false
}

func secretConsumedAsStorageSSH(name string, state v1alpha1.State) bool {
	return secretConsumedAsStorageSSHPublic(name, state) || secretConsumedAsStorageSSHPrivate(name, state)
}

func secretConsumedAsStorageSSHPublic(name string, state v1alpha1.State) bool {
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil {
			continue
		}
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			machine, ok := topology.NodeMachine(state, cluster, node.Name)
			if ok && machine.Spec.OS.SSH != nil && machine.Spec.OS.SSH.KeyRef.Name == name {
				return true
			}
		}
	}
	return false
}

func secretConsumedAsStorageSSHPrivate(name string, state v1alpha1.State) bool {
	return secretConsumedAsStorageSSHPublic(name, state)
}

func secretConsumedAsHostSSH(name string, state v1alpha1.State) bool {
	for _, machine := range state.Machines {
		if machine.Spec.OS.SSH != nil && machine.Spec.OS.SSH.KeyRef.Name == name {
			return true
		}
	}
	return false
}

func secretPathsPresent(paths []secretPathEntry) (bool, string) {
	for _, path := range paths {
		stat := secret.Stat
		if path.externalSource {
			stat = secret.StatExternalFile
		}
		info, err := stat(path.path)
		if errors.Is(err, os.ErrNotExist) {
			return false, "missing " + path.path
		}
		if err != nil {
			return false, fmt.Sprintf("stat %s: %v", path.path, err)
		}
		if info.IsDir() {
			return false, path.path + " is a directory"
		}
	}
	return true, ""
}
