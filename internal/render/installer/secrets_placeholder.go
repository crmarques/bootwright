package installer

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// PlaceholderInstallerSecrets returns sentinel strings for placeholder
// installer output in place of real material.
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

// PortableInstallerSecrets returns "{{ secret <name>[.<role>] }}" substitution
// tokens for the context-free portable render. Unlike PlaceholderInstallerSecrets
// (whose "<bootwright-...-ref:>" sentinels match the context placeholder render),
// these tokens are meant for a downstream secrets manager to rehydrate. Like the
// placeholder generator it reads no material and takes no secrets dir.
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
	// vSphere user/password come from one credentials file (no MaterialRole), so
	// populate VSphereCredentials with .username/.password sub-tokens here; the
	// existing embed at vSphereVCenterConfig then carries them unchanged instead
	// of falling back to the "<bootwright-vsphere-*-ref:>" sentinels.
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

// CheckPortableSupport rejects the install-config secret material the portable
// render cannot faithfully represent as a single {{ secret }} token: the
// context render bakes disconnected mirror-registry credentials into the
// pull-secret JSON and authenticated cluster-install proxy credentials into the
// proxy URL userinfo. A portable bundle has no token form for either (the
// pull-secret is already one whole-value token; the proxy URL would need a
// merged userinfo), so rather than silently omit them — producing an install
// that cannot reach its mirror or proxy — the portable render fails fast and
// points the operator at the context render.
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
