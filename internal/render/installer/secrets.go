package installer

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infra/proxy"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// InstallerSecrets holds secret material inlined into install-config.yaml
// when ResolveInstaller is run with a real secrets directory.
type InstallerSecrets struct {
	PullSecret  string
	SSHKey      string
	TrustBundle string
	ProxyHTTP   string
	ProxyHTTPS  string
	TLSPairs    map[string]InstallerTLSSecret
	// VSphereCredentials carries vCenter user/password material keyed by
	// credentialsRef name; an absent entry keeps the placeholder render.
	VSphereCredentials map[string]InstallerUserPass
}

type InstallerTLSSecret struct {
	Cert string
	Key  string
}

type InstallerUserPass struct {
	Username string
	Password string
}

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

func LoadInstallerSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster, secretsDir string) (InstallerSecrets, error) {
	return LoadInstallerSecretsForContext("test", state, ocp, secretsDir)
}

func LoadInstallerSecretsForContext(contextName string, state v1alpha1.State, ocp v1alpha1.ContainerCluster, secretsDir string) (InstallerSecrets, error) {
	env := stateview.Environment(state)
	resolver := secret.NewResolver(contextName, secretsDir, env)
	var out InstallerSecrets

	pullName := ocp.Spec.Install.PullSecretRef.Name
	if pullName == "" && v1alpha1.DistributionType(ocp) == v1alpha1.DistributionOpenShift {
		return out, fmt.Errorf("%s: pullSecretRef is empty; declare %s in Environment.spec.secrets or set ContainerCluster.spec.install.pullSecretRef", ocp.Metadata.Name, v1alpha1.DefaultPullSecretName)
	}
	if pullName != "" {
		pullPath, pullSecret, err := resolver.ReadMaterialWithPath(pullName, secret.MaterialPrimary, "pull secret")
		if err != nil {
			return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
		}
		if err := validatePullSecret(pullSecret, pullPath); err != nil {
			return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
		}
		out.PullSecret = pullSecret
	} else {
		out.PullSecret = "{}"
	}

	sshName := ocp.Spec.Install.NodeSSH.PublicMaterialRef().Name
	if sshName == "" {
		return out, fmt.Errorf("%s: nodeSSH public key is empty; declare %s in Environment.spec.secrets or set ContainerCluster.spec.install.nodeSSH", ocp.Metadata.Name, v1alpha1.ClusterAdminSSHKeyName(ocp.Metadata.Name))
	}
	_, sshKey, err := resolver.ReadMaterialWithPath(sshName, secret.MaterialSSHPublic, "cluster admin public key")
	if err != nil {
		return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
	}
	out.SSHKey = sshKey

	if refs := additionalTrustBundleRefs(state, ocp); len(refs) > 0 {
		var bundles []string
		for _, ref := range refs {
			name := ref.Name
			tbPath, bundle, err := resolver.ReadMaterialWithPath(name, secret.MaterialPrimary, "additional trust bundle")
			if err != nil {
				return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
			}
			if err := secret.ValidateCABundlePEM([]byte(bundle)); err != nil {
				return out, fmt.Errorf("%s: additional trust bundle %s at %s: %w", ocp.Metadata.Name, name, tbPath, err)
			}
			bundles = append(bundles, strings.TrimSpace(bundle))
		}
		out.TrustBundle = strings.Join(bundles, "\n")
	}

	if refs := servingCertificateSecretRefs(ocp); len(refs) > 0 {
		out.TLSPairs = map[string]InstallerTLSSecret{}
		for _, ref := range refs {
			_, certPEM, err := resolver.ReadMaterialWithPath(ref.Name, secret.MaterialPrimary, "serving certificate")
			if err != nil {
				return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
			}
			_, keyPEM, err := resolver.ReadMaterialWithPath(ref.Name, secret.MaterialTLSKey, "serving certificate private key")
			if err != nil {
				return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
			}
			if _, err := secret.ValidateTLSCertificateKey([]byte(certPEM), []byte(keyPEM)); err != nil {
				return out, fmt.Errorf("%s: serving certificate %s: %w", ocp.Metadata.Name, ref.Name, err)
			}
			out.TLSPairs[ref.Name] = InstallerTLSSecret{Cert: certPEM, Key: keyPEM}
		}
		if err := validateServingCertificateCoverage(state, ocp, out.TLSPairs); err != nil {
			return out, err
		}
	}

	if env == nil {
		return out, nil
	}
	ci, err := ClusterInstallForOCP(state, ocp)
	if err != nil {
		return out, err
	}
	if clusterPlatformKind(ci, ocp) == v1alpha1.PlatformTypeVSphere {
		creds, err := loadVSphereCredentials(state, ci, resolver)
		if err != nil {
			return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
		}
		out.VSphereCredentials = creds
	}
	if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		if reg := env.Spec.Registries; reg != nil && reg.Mirror != nil && reg.Mirror.CredentialsRef.Name != "" {
			creds, err := readUserPassMaterial(resolver, reg.Mirror.CredentialsRef.Name, secret.MaterialPrimary, "mirror registry credentials")
			if err != nil {
				return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
			}
			mirrorURL := effectiveMirrorRegistryURL(state, ci, env)
			if mirrorURL == "" {
				return out, fmt.Errorf("%s: disconnected mirror credentialsRef is set, but no mirror registry URL is declared or derivable", ocp.Metadata.Name)
			}
			merged, err := mergeMirrorAuth(out.PullSecret, mirrorURL, creds)
			if err != nil {
				return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
			}
			out.PullSecret = merged
		}
	}
	eff, managedURL, err := clusterInstallProxyInputs(state, env, ci)
	if err != nil {
		return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
	}
	if eff != nil || managedURL != "" {
		if eff == nil {
			eff = &proxy.Effective{}
		}
		fallbackURL := ""
		if eff.HTTP == "" || eff.HTTPS == "" {
			fallbackURL = managedURL
		}
		httpURL := pick(eff.HTTP, fallbackURL)
		httpsURL := pick(eff.HTTPS, fallbackURL)
		if eff.Auth.Name != "" && (httpURL != "" || httpsURL != "") {
			creds, err := readUserPassMaterial(resolver, eff.Auth.Name, secret.MaterialPrimary, "proxy credentials")
			if err != nil {
				return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
			}
			if httpURL != "" {
				httpURL, err = bakeProxyCredentials(httpURL, creds)
				if err != nil {
					return out, fmt.Errorf("%s: httpProxy: %w", ocp.Metadata.Name, err)
				}
			}
			if httpsURL != "" {
				httpsURL, err = bakeProxyCredentials(httpsURL, creds)
				if err != nil {
					return out, fmt.Errorf("%s: httpsProxy: %w", ocp.Metadata.Name, err)
				}
			}
		}
		out.ProxyHTTP = httpURL
		out.ProxyHTTPS = httpsURL
	}
	return out, nil
}

func pick(declared, derived string) string {
	if declared != "" {
		return declared
	}
	return derived
}

func additionalTrustBundleRefs(state v1alpha1.State, ocp v1alpha1.ContainerCluster) []v1alpha1.SecretRef {
	env := stateview.Environment(state)
	var refs []v1alpha1.SecretRef
	if env != nil {
		if env.Spec.InstallTrust != nil {
			refs = append(refs, env.Spec.InstallTrust.CABundleRefs...)
		}
		if reg := env.Spec.Registries; reg != nil && reg.Mirror != nil && reg.Mirror.TrustBundleRef.Name != "" {
			refs = append(refs, reg.Mirror.TrustBundleRef)
		}
	}
	refs = append(refs, ocp.Spec.Install.AdditionalTrustBundleRefs...)
	seen := map[string]bool{}
	out := make([]v1alpha1.SecretRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Name == "" || seen[ref.Name] {
			continue
		}
		seen[ref.Name] = true
		out = append(out, ref)
	}
	return out
}

func servingCertificateSecretRefs(ocp v1alpha1.ContainerCluster) []v1alpha1.SecretRef {
	serving := ocp.Spec.Install.ServingCertificates
	if serving == nil {
		return nil
	}
	var refs []v1alpha1.SecretRef
	if api := serving.APIServer; api != nil {
		for _, cert := range api.NamedCertificates {
			refs = append(refs, cert.SecretRef)
		}
	}
	if ingress := serving.Ingress; ingress != nil {
		refs = append(refs, ingress.DefaultCertificateRef)
	}
	seen := map[string]bool{}
	out := make([]v1alpha1.SecretRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Name == "" || seen[ref.Name] {
			continue
		}
		seen[ref.Name] = true
		out = append(out, ref)
	}
	return out
}

func validateServingCertificateCoverage(state v1alpha1.State, ocp v1alpha1.ContainerCluster, pairs map[string]InstallerTLSSecret) error {
	serving := ocp.Spec.Install.ServingCertificates
	if serving == nil {
		return nil
	}
	if api := serving.APIServer; api != nil {
		for _, cert := range api.NamedCertificates {
			pair := pairs[cert.SecretRef.Name]
			for _, name := range cert.Names {
				if err := secret.CertificateBundleCoversDNSName([]byte(pair.Cert), name); err != nil {
					return fmt.Errorf("%s: serving certificate %s does not cover API name %s: %w", ocp.Metadata.Name, cert.SecretRef.Name, name, err)
				}
			}
		}
	}
	if ingress := serving.Ingress; ingress != nil && ingress.DefaultCertificateRef.Name != "" {
		env := stateview.Environment(state)
		baseDomain := ""
		if env != nil {
			baseDomain = env.Spec.BaseDomain
		}
		probe := stateview.ClusterConsoleHostname(ocp.Metadata.Name, baseDomain)
		pair := pairs[ingress.DefaultCertificateRef.Name]
		if err := secret.CertificateBundleCoversDNSName([]byte(pair.Cert), probe); err != nil {
			return fmt.Errorf("%s: serving certificate %s does not cover ingress wildcard *.apps.%s.%s: %w", ocp.Metadata.Name, ingress.DefaultCertificateRef.Name, ocp.Metadata.Name, baseDomain, err)
		}
	}
	return nil
}

func validatePullSecret(content, path string) error {
	var doc struct {
		Auths map[string]any `json:"auths"`
	}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return fmt.Errorf("pull secret %s is empty", path)
	}
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return fmt.Errorf("pull secret %s is not valid JSON: %w", path, err)
	}
	if doc.Auths == nil {
		return fmt.Errorf("pull secret %s is missing .auths object", path)
	}
	return nil
}

type userPass struct {
	Username string
	Password string
}

func readUserPassFile(path, kind string) (userPass, error) {
	creds, err := secret.ReadUserPasswordFile(path, kind)
	if err != nil {
		return userPass{}, err
	}
	return userPass{Username: creds.Username, Password: creds.Password}, nil
}

func readUserPassMaterial(resolver secret.Resolver, name string, role secret.MaterialRole, kind string) (userPass, error) {
	creds, err := resolver.ReadUserPasswordMaterial(name, role, kind)
	if err != nil {
		return userPass{}, err
	}
	return userPass{Username: creds.Username, Password: creds.Password}, nil
}

func mergeMirrorAuth(pullSecret, registryURL string, creds userPass) (string, error) {
	if strings.TrimSpace(registryURL) == "" {
		return "", errors.New("merge mirror auth: registry URL is empty")
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(pullSecret), &doc); err != nil {
		return "", fmt.Errorf("merge mirror auth: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	auths, _ := doc["auths"].(map[string]any)
	if auths == nil {
		auths = map[string]any{}
		doc["auths"] = auths
	}
	auth := base64.StdEncoding.EncodeToString([]byte(creds.Username + ":" + creds.Password))
	auths[registryURL] = map[string]any{"auth": auth}
	out, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func bakeProxyCredentials(rawURL string, creds userPass) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errors.New("proxy URL must include scheme and host")
	}
	u.User = url.UserPassword(creds.Username, creds.Password)
	return u.String(), nil
}
