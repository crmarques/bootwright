package render

import (
	"fmt"
	"sort"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secret"
)

type InstallerSecretInputStat struct {
	Label           string `json:"label"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	Size            int64  `json:"size"`
	Mode            uint32 `json:"mode"`
	ModTimeUnixNano int64  `json:"modTimeUnixNano"`
}

func InstallerSecretInputStats(state v1alpha1.State, ocp v1alpha1.ContainerCluster, secretsDir string) ([]InstallerSecretInputStat, error) {
	env := primaryEnvironment(state)
	refs := installerSecretRefs(state, ocp, env)
	out := make([]InstallerSecretInputStat, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref.name == "" {
			continue
		}
		path := secret.ResolvePath(ref.name, env, secretsDir)
		if ref.tlsKey {
			path = secret.ResolveTLSKeyPath(ref.name, env, secretsDir)
		}
		key := ref.label + "\x00" + ref.name + "\x00" + path
		if seen[key] {
			continue
		}
		seen[key] = true
		info, err := secret.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("%s %s at %s: %w", ocp.Metadata.Name, ref.label, path, err)
		}
		out = append(out, InstallerSecretInputStat{
			Label:           ref.label,
			Name:            ref.name,
			Path:            path,
			Size:            info.Size(),
			Mode:            uint32(info.Mode().Perm()),
			ModTimeUnixNano: info.ModTime().UnixNano(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Label != out[j].Label {
			return out[i].Label < out[j].Label
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

type installerSecretRef struct {
	label  string
	name   string
	tlsKey bool
}

func installerSecretRefs(state v1alpha1.State, ocp v1alpha1.ContainerCluster, env *v1alpha1.Environment) []installerSecretRef {
	refs := []installerSecretRef{
		{label: "pullSecretRef", name: ocp.Spec.Install.PullSecretRef.Name},
		{label: "sshKeyRef", name: ocp.Spec.Install.SSHKeyRef.Name},
	}
	for _, ref := range additionalTrustBundleRefs(state, ocp) {
		refs = append(refs, installerSecretRef{label: "additionalTrustBundleRef", name: ref.Name})
	}
	if env != nil && v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		if reg := env.Spec.Registries; reg != nil && reg.Mirror != nil {
			refs = append(refs, installerSecretRef{label: "mirrorCredentialsRef", name: reg.Mirror.CredentialsRef.Name})
		}
	}
	if env != nil {
		ci, err := clusterInfraForOCP(state, ocp)
		if err == nil {
			if eff, _, err := clusterInstallProxyInputs(state, env, ci); err == nil && eff != nil {
				refs = append(refs, installerSecretRef{label: "proxyAuthRef", name: eff.Auth.Name})
			}
		}
	}
	for _, ref := range servingCertificateSecretRefs(ocp) {
		refs = append(refs,
			installerSecretRef{label: "servingCertificateRef", name: ref.Name},
			installerSecretRef{label: "servingCertificateKeyRef", name: ref.Name, tlsKey: true},
		)
	}
	return refs
}
