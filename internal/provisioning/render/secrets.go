package render

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/proxy"
	"github.com/crmarques/bootwright/internal/secret"
)

// InstallerSecrets holds secret material inlined into install-config.yaml
// when ResolveInstaller is run with a real secrets directory.
type InstallerSecrets struct {
	PullSecret  string
	SSHKey      string
	TrustBundle string
	ProxyHTTP   string
	ProxyHTTPS  string
}

// PlaceholderInstallerSecrets returns sentinel strings for placeholder
// installer output in place of real material.
func PlaceholderInstallerSecrets(ocp v1alpha1.ContainerCluster) InstallerSecrets {
	pullSecret := "{}"
	if ocp.Spec.Install.PullSecretRef.Name != "" {
		pullSecret = pullSecretPlaceholder(ocp.Spec.Install.PullSecretRef.Name)
	}
	out := InstallerSecrets{
		PullSecret: pullSecret,
		SSHKey:     secretRefPlaceholder("ssh-key", ocp.Spec.Install.SSHKeyRef.Name),
	}
	if ocp.Spec.Install.AdditionalTrustBundleRef.Name != "" {
		out.TrustBundle = secretRefPlaceholder("trust-bundle", ocp.Spec.Install.AdditionalTrustBundleRef.Name)
	}
	return out
}

func LoadInstallerSecrets(state v1alpha1.State, ocp v1alpha1.ContainerCluster, secretsDir string) (InstallerSecrets, error) {
	env := primaryEnvironment(state)
	var out InstallerSecrets

	pullName := ocp.Spec.Install.PullSecretRef.Name
	if pullName == "" && v1alpha1.DistributionType(ocp) == v1alpha1.DistributionOpenShift {
		return out, fmt.Errorf("%s: pullSecretRef is empty; declare Environment.spec.secrets.%s or set ContainerCluster.spec.install.pullSecretRef", ocp.Metadata.Name, v1alpha1.DefaultPullSecretName)
	}
	if pullName != "" {
		pullPath := secret.ResolvePath(pullName, env, secretsDir)
		pullSecret, err := readSecretFile(pullPath, "pull secret")
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

	sshName := ocp.Spec.Install.SSHKeyRef.Name
	if sshName == "" {
		return out, fmt.Errorf("%s: sshKeyRef is empty; declare Environment.spec.secrets.%s or set ContainerCluster.spec.install.sshKeyRef", ocp.Metadata.Name, v1alpha1.DefaultClusterSSHKeyName)
	}
	sshPath := secret.ResolvePath(sshName, env, secretsDir)
	sshKey, err := readSecretFile(sshPath, "cluster admin public key")
	if err != nil {
		return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
	}
	out.SSHKey = sshKey

	if name := ocp.Spec.Install.AdditionalTrustBundleRef.Name; name != "" {
		tbPath := secret.ResolvePath(name, env, secretsDir)
		bundle, err := readSecretFile(tbPath, "additional trust bundle")
		if err != nil {
			return out, fmt.Errorf("%s: %w", ocp.Metadata.Name, err)
		}
		out.TrustBundle = bundle
	}

	if env == nil {
		return out, nil
	}
	ci, err := clusterInfraForOCP(state, ocp)
	if err != nil {
		return out, err
	}
	if v1alpha1.InstallMode(ocp) == v1alpha1.InstallModeDisconnected {
		if reg := env.Spec.Registries; reg != nil && reg.Mirror != nil && reg.Mirror.CredentialsRef.Name != "" {
			credPath := secret.ResolvePath(reg.Mirror.CredentialsRef.Name, env, secretsDir)
			creds, err := readUserPassFile(credPath, "mirror registry credentials")
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
	if eff, managedURL := clusterInstallProxyInputs(state, env, ci); eff != nil || managedURL != "" {
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
			credPath := secret.ResolvePath(eff.Auth.Name, env, secretsDir)
			creds, err := readUserPassFile(credPath, "proxy credentials")
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

func readSecretFile(path, kind string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%s path is empty", kind)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s at %s: %w", kind, path, err)
	}
	return strings.TrimRight(string(data), "\n"), nil
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
