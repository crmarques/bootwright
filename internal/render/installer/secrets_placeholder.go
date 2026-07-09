package installer

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func PlaceholderInstallerSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster) InstallerSecrets {
	pullSecret := "{}"
	if ocp.Spec.Install.PullSecretRef.Name != "" {
		pullSecret = pullSecretPlaceholder(ocp.Spec.Install.PullSecretRef.Name)
	}
	out := InstallerSecrets{
		PullSecret: pullSecret,
		SSHKey:     secretRefPlaceholder("ssh-key", ocp.Spec.Install.NodeSSH.PublicMaterialRef().Name),
		TLSPairs:   map[string]InstallerTLSSecret{},
	}
	if refs := additionalTrustBundleRefs(state, ocp); len(refs) > 0 {
		var parts []string
		for _, ref := range refs {
			parts = append(parts, secretRefPlaceholder("trust-bundle", ref.Name))
		}
		out.TrustBundle = strings.Join(parts, "\n")
	}
	for _, ref := range servingCertificateSecretRefs(ocp) {
		out.TLSPairs[ref.Name] = InstallerTLSSecret{
			Cert: secretRefPlaceholder("tls.crt", ref.Name),
			Key:  secretRefPlaceholder("tls.key", ref.Name),
		}
	}
	return out
}

func PortableInstallerSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster) InstallerSecrets {
	pullSecret := "{}"
	if ocp.Spec.Install.PullSecretRef.Name != "" {
		pullSecret = secret.SecretPlaceholder(ocp.Spec.Install.PullSecretRef.Name, "")
	}
	out := InstallerSecrets{
		PullSecret: pullSecret,
		SSHKey:     secret.SecretPlaceholder(ocp.Spec.Install.NodeSSH.PublicMaterialRef().Name, string(secret.MaterialSSHPublic)),
		TLSPairs:   map[string]InstallerTLSSecret{},
	}
	if refs := additionalTrustBundleRefs(state, ocp); len(refs) > 0 {
		var parts []string
		for _, ref := range refs {
			parts = append(parts, secret.SecretPlaceholder(ref.Name, ""))
		}
		out.TrustBundle = strings.Join(parts, "\n")
	}
	for _, ref := range servingCertificateSecretRefs(ocp) {
		out.TLSPairs[ref.Name] = InstallerTLSSecret{
			Cert: secret.SecretPlaceholder(ref.Name, ""),
			Key:  secret.SecretPlaceholder(ref.Name, string(secret.MaterialTLSKey)),
		}
	}
	if ci, err := ClusterInstallForOCP(state, ocp); err == nil {
		if names := vSphereCredentialRefNames(state, ci); len(names) > 0 {
			out.VSphereCredentials = map[string]InstallerUserPass{}
			for _, name := range names {
				out.VSphereCredentials[name] = InstallerUserPass{
					Username: secret.SecretPlaceholder(name, "username"),
					Password: secret.SecretPlaceholder(name, "password"),
				}
			}
		}
	}
	return out
}

func CheckPortableSupport(state v1alpha1.State, ocp v1alpha1.ContainerCluster) error {
	env := stateview.Environment(state)
	if env != nil && v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		if reg := env.Spec.Registries; reg != nil && reg.Mirror != nil && reg.Mirror.CredentialsRef.Name != "" {
			return fmt.Errorf("%s: portable render (--input-dir) cannot tokenize disconnected mirror-registry credentials (registries.mirror.credentialsRef %q); render with a configured context (render --output-dir --sensitive) instead", ocp.Metadata.Name, reg.Mirror.CredentialsRef.Name)
		}
	}
	ci, err := ClusterInstallForOCP(state, ocp)
	if err != nil {
		return err
	}
	eff, managedURL, err := clusterInstallProxyInputs(state, env, ci)
	if err != nil {
		return err
	}
	if eff != nil && eff.Auth.Name != "" && (eff.HTTP != "" || eff.HTTPS != "" || managedURL != "") {
		return fmt.Errorf("%s: portable render (--input-dir) cannot tokenize authenticated cluster-install proxy credentials (proxy credentialsRef %q); render with a configured context (render --output-dir --sensitive) instead", ocp.Metadata.Name, eff.Auth.Name)
	}
	return nil
}
