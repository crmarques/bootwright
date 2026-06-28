package installer_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// TestPortableInstallerSecretsEmitsJinjaTokens pins that the portable generator
// renders pull-secret, node SSH and serving-certificate refs as
// {{ secret <name>[.<role>] }} tokens (not the "<bootwright-...-ref:>" sentinels
// PlaceholderInstallerSecrets uses), and that a portable TLS token lands in the
// Secret's stringData verbatim rather than the base64 data block.
func TestPortableInstallerSecretsEmitsJinjaTokens(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	ocp := &state.ContainerClusters[0]
	clusterName := ocp.Metadata.Name
	baseDomain := state.Environments[0].Spec.BaseDomain
	state.Environments[0].Spec.Secrets["api-tls"] = v1alpha1.EnvironmentSecretSpec{}
	ocp.Spec.Install.ServingCertificates = &v1alpha1.ServingCertificatesSpec{
		APIServer: &v1alpha1.APIServerServingCertificateSpec{
			NamedCertificates: []v1alpha1.APIServerNamedCertificateSpec{{
				Names:     []string{"api." + clusterName + "." + baseDomain},
				SecretRef: v1alpha1.SecretRef{Name: "api-tls"},
			}},
		},
	}

	secrets := installer.PortableInstallerSecrets(state, *ocp)

	pullRef := ocp.Spec.Install.PullSecretRef.Name
	if want := "{{ secret " + pullRef + " }}"; secrets.PullSecret != want {
		t.Errorf("PullSecret = %q want %q", secrets.PullSecret, want)
	}
	sshRef := ocp.Spec.Install.NodeSSH.PublicMaterialRef().Name
	if want := "{{ secret " + sshRef + ".ssh-public }}"; secrets.SSHKey != want {
		t.Errorf("SSHKey = %q want %q", secrets.SSHKey, want)
	}
	pair := secrets.TLSPairs["api-tls"]
	if pair.Cert != "{{ secret api-tls }}" {
		t.Errorf("TLS cert token = %q", pair.Cert)
	}
	if pair.Key != "{{ secret api-tls.tls-key }}" {
		t.Errorf("TLS key token = %q", pair.Key)
	}
	if strings.Contains(secrets.PullSecret, "<bootwright-") {
		t.Error("portable PullSecret leaked the <bootwright-...> sentinel format")
	}

	// The portable cert/key tokens must be detected as placeholders so they land
	// in stringData verbatim (base64-encoding them into data would corrupt them).
	tlsManifest := apiServingCertManifest(t, installer.InstallerManifests(*ocp, secrets))
	if _, ok := tlsManifest["data"]; ok {
		t.Fatalf("portable TLS Secret must not carry base64 data: %v", tlsManifest)
	}
	stringData, ok := tlsManifest["stringData"].(map[string]any)
	if !ok {
		t.Fatalf("portable TLS Secret missing stringData: %v", tlsManifest)
	}
	if stringData["tls.crt"] != pair.Cert || stringData["tls.key"] != pair.Key {
		t.Fatalf("stringData did not carry the tokens verbatim: %v", stringData)
	}
}

// TestPortableInstallerSecretsVSphereCredentials pins that vCenter credentials
// render as .username/.password sub-tokens carried through the existing
// vSphereVCenterConfig embed.
func TestPortableInstallerSecretsVSphereCredentials(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "007-sno-vsphere")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	ocp := state.ContainerClusters[0]
	secrets := installer.PortableInstallerSecrets(state, ocp)
	if len(secrets.VSphereCredentials) == 0 {
		t.Fatal("expected vCenter credential placeholders for a vSphere cluster")
	}
	for name, creds := range secrets.VSphereCredentials {
		if want := "{{ secret " + name + ".username }}"; creds.Username != want {
			t.Errorf("vCenter %s user = %q want %q", name, creds.Username, want)
		}
		if want := "{{ secret " + name + ".password }}"; creds.Password != want {
			t.Errorf("vCenter %s password = %q want %q", name, creds.Password, want)
		}
	}

	config, err := installer.InstallerConfigWithSecrets(state, ocp, secrets)
	if err != nil {
		t.Fatalf("InstallerConfigWithSecrets: %v", err)
	}
	rendered := flattenStrings(config)
	if !strings.Contains(rendered, "{{ secret ") || strings.Contains(rendered, "<bootwright-vsphere-") {
		t.Errorf("install-config vCenter creds did not carry portable tokens: %s", rendered)
	}
}

// TestCheckPortableSupport pins the fail-fast guard: a clean cluster is
// supported, but an authenticated cluster-install proxy (whose credentials have
// no portable token form) is rejected.
func TestCheckPortableSupport(t *testing.T) {
	base, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	ocp := base.ContainerClusters[0]

	if err := installer.CheckPortableSupport(base, ocp); err != nil {
		t.Fatalf("clean cluster rejected: %v", err)
	}

	proxied := base
	env := base.Environments[0]
	env.Spec.ProxyFor.ContainerClusterInstall = "default"
	env.Spec.InfraComponents.Proxies = []v1alpha1.EnvironmentProxyComponent{{
		Name:       "default",
		Management: v1alpha1.EnvironmentComponentExternal,
		Connection: &v1alpha1.EnvironmentProxyConnection{
			HTTPProxy:  "http://external-proxy:3128",
			HTTPSProxy: "https://external-proxy:3128",
			Auth:       &v1alpha1.EnvironmentProxyAuthSpec{ProxyAuthRef: v1alpha1.SecretRef{Name: "proxy-auth"}},
		},
	}}
	proxied.Environments = []v1alpha1.Environment{env}
	err = installer.CheckPortableSupport(proxied, ocp)
	if err == nil || !strings.Contains(err.Error(), "proxy") {
		t.Fatalf("authenticated proxy not rejected: %v", err)
	}
}

func apiServingCertManifest(t *testing.T, manifests []installer.InstallerManifest) map[string]any {
	t.Helper()
	for _, m := range manifests {
		if m.Object["kind"] == "Secret" && m.Object["type"] == "kubernetes.io/tls" {
			return m.Object
		}
	}
	t.Fatalf("no TLS Secret manifest among %d manifests", len(manifests))
	return nil
}

// flattenStrings concatenates every string leaf in a config map so a test can
// substring-assert without a YAML dependency.
func flattenStrings(v any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case map[string]any:
			for _, val := range t {
				walk(val)
			}
		case []any:
			for _, val := range t {
				walk(val)
			}
		case string:
			b.WriteString(t)
			b.WriteByte('\n')
		}
	}
	walk(v)
	return b.String()
}
