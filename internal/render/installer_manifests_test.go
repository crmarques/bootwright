package render_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/desired"
	"go.yaml.in/yaml/v3"
)

func TestResolveInstallerRendersTrustBundleAndServingCertificateManifests(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	clusterName := state.ContainerClusters[0].Metadata.Name
	baseDomain := state.Environments[0].Spec.BaseDomain
	state.Environments[0].Spec.Secrets["pull"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["ssh"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["env-ca"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["mirror-ca"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["cluster-ca"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["api-tls"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["ingress-tls"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.InstallTrust = &v1alpha1.EnvironmentInstallTrustSpec{
		CABundleRefs: []v1alpha1.SecretRef{{Name: "env-ca"}},
	}
	state.Environments[0].Spec.Registries = &v1alpha1.EnvironmentRegistriesSpec{
		Mirror: &v1alpha1.EnvironmentRegistryMirrorSpec{
			TrustBundleRef: v1alpha1.SecretRef{Name: "mirror-ca"},
		},
	}
	state.ContainerClusters[0].Spec.Install.PullSecretRef = v1alpha1.SecretRef{Name: "pull"}
	state.ContainerClusters[0].Spec.Install.NodeSSH = v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ssh"}}
	state.ContainerClusters[0].Spec.Install.AdditionalTrustBundleRefs = []v1alpha1.SecretRef{{Name: "cluster-ca"}, {Name: "env-ca"}}
	state.ContainerClusters[0].Spec.Install.ServingCertificates = &v1alpha1.ServingCertificatesSpec{
		APIServer: &v1alpha1.APIServerServingCertificateSpec{
			NamedCertificates: []v1alpha1.APIServerNamedCertificateSpec{{
				Names:     []string{"api." + clusterName + "." + baseDomain},
				SecretRef: v1alpha1.SecretRef{Name: "api-tls"},
			}},
		},
		Ingress: &v1alpha1.IngressServingCertificateSpec{
			DefaultCertificateRef: v1alpha1.SecretRef{Name: "ingress-tls"},
		},
	}

	secretsDir := t.TempDir()
	writeInstallerCoreSecrets(t, secretsDir)
	envCA, _ := writeSelfSignedPair(t, secretsDir, "env-ca", v1alpha1.SelfSignedCertificateSpec{CommonName: "env-ca", ValidityDays: 1})
	mirrorCA, _ := writeSelfSignedPair(t, secretsDir, "mirror-ca", v1alpha1.SelfSignedCertificateSpec{CommonName: "mirror-ca", ValidityDays: 1})
	clusterCA, _ := writeSelfSignedPair(t, secretsDir, "cluster-ca", v1alpha1.SelfSignedCertificateSpec{CommonName: "cluster-ca", ValidityDays: 1})
	apiCert, _ := writeSelfSignedPair(t, secretsDir, "api-tls", v1alpha1.SelfSignedCertificateSpec{
		CommonName:   "api." + clusterName + "." + baseDomain,
		DNSNames:     []string{"api." + clusterName + "." + baseDomain},
		ValidityDays: 1,
	})
	ingressCert, ingressKey := writeSelfSignedPair(t, secretsDir, "ingress-tls", v1alpha1.SelfSignedCertificateSpec{
		CommonName:   "*.apps." + clusterName + "." + baseDomain,
		DNSNames:     []string{"*.apps." + clusterName + "." + baseDomain},
		ValidityDays: 1,
	})

	clustersDir := t.TempDir()
	result, err := render.ResolveInstaller(clustersDir, secretsDir, state)
	if err != nil {
		t.Fatalf("ResolveInstaller: %v", err)
	}
	asset := result.InstallerAssets[0]
	body, err := os.ReadFile(asset.EffectiveInstallConfigPath)
	if err != nil {
		t.Fatalf("read install-config: %v", err)
	}
	var installConfig map[string]any
	if err := yaml.Unmarshal(body, &installConfig); err != nil {
		t.Fatalf("decode install-config: %v", err)
	}
	bundle := installConfig["additionalTrustBundle"].(string)
	envIdx := strings.Index(bundle, strings.TrimSpace(string(envCA)))
	mirrorIdx := strings.Index(bundle, strings.TrimSpace(string(mirrorCA)))
	clusterIdx := strings.Index(bundle, strings.TrimSpace(string(clusterCA)))
	if envIdx < 0 || mirrorIdx < 0 || clusterIdx < 0 {
		t.Fatalf("install-config missing expected CA bundles")
	}
	if !(envIdx < mirrorIdx && mirrorIdx < clusterIdx) {
		t.Fatalf("CA bundle order got env=%d mirror=%d cluster=%d", envIdx, mirrorIdx, clusterIdx)
	}
	if strings.Count(bundle, strings.TrimSpace(string(envCA))) != 1 {
		t.Fatalf("env-ca was not de-duplicated in install-config")
	}

	apiSecret := readManifest(t, filepath.Join(asset.EffectiveInstallManifestsDir, "50-bootwright-api-serving-cert-api-tls.yaml"))
	data := apiSecret["data"].(map[string]any)
	gotAPICert, err := base64.StdEncoding.DecodeString(data["tls.crt"].(string))
	if err != nil {
		t.Fatalf("decode API cert data: %v", err)
	}
	if strings.TrimSpace(string(gotAPICert)) != strings.TrimSpace(string(apiCert)) {
		t.Fatalf("API cert data did not round-trip")
	}
	if _, ok := apiSecret["stringData"]; ok {
		t.Fatalf("effective Secret must not use stringData: %v", apiSecret)
	}
	ingressSecret := readManifest(t, filepath.Join(asset.EffectiveInstallManifestsDir, "60-bootwright-ingress-default-cert-ingress-tls.yaml"))
	ingressData := ingressSecret["data"].(map[string]any)
	gotIngressKey, err := base64.StdEncoding.DecodeString(ingressData["tls.key"].(string))
	if err != nil {
		t.Fatalf("decode ingress key data: %v", err)
	}
	if strings.TrimSpace(string(gotIngressKey)) != strings.TrimSpace(string(ingressKey)) || !strings.Contains(string(ingressCert), "BEGIN CERTIFICATE") {
		t.Fatalf("ingress TLS material did not round-trip")
	}
}

func TestPlaceholderInstallerRendersRedactedServingCertificateManifests(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	clusterName := state.ContainerClusters[0].Metadata.Name
	baseDomain := state.Environments[0].Spec.BaseDomain
	state.Environments[0].Spec.Secrets["api-tls"] = v1alpha1.EnvironmentSecretSpec{}
	state.ContainerClusters[0].Spec.Install.ServingCertificates = &v1alpha1.ServingCertificatesSpec{
		APIServer: &v1alpha1.APIServerServingCertificateSpec{
			NamedCertificates: []v1alpha1.APIServerNamedCertificateSpec{{
				Names:     []string{"api." + clusterName + "." + baseDomain},
				SecretRef: v1alpha1.SecretRef{Name: "api-tls"},
			}},
		},
	}

	result, err := render.All(t.TempDir(), t.TempDir(), t.TempDir(), state)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	asset := result.InstallerAssets[0]
	apiSecret := readManifest(t, filepath.Join(asset.InstallManifestsDir, "50-bootwright-api-serving-cert-api-tls.yaml"))
	stringData := apiSecret["stringData"].(map[string]any)
	if got := stringData["tls.crt"].(string); got != "<bootwright-tls.crt-ref:api-tls>" {
		t.Fatalf("placeholder tls.crt got %q", got)
	}
	if _, ok := apiSecret["data"]; ok {
		t.Fatalf("placeholder Secret must not include base64 data: %v", apiSecret)
	}
}

func TestResolveInstallerRejectsServingCertificateMismatch(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	state.Environments[0].Spec.Secrets["pull"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["ssh"] = v1alpha1.EnvironmentSecretSpec{}
	state.Environments[0].Spec.Secrets["ingress-tls"] = v1alpha1.EnvironmentSecretSpec{}
	state.ContainerClusters[0].Spec.Install.PullSecretRef = v1alpha1.SecretRef{Name: "pull"}
	state.ContainerClusters[0].Spec.Install.NodeSSH = v1alpha1.NodeSSHSpec{KeyPairRef: v1alpha1.SecretRef{Name: "ssh"}}
	state.ContainerClusters[0].Spec.Install.ServingCertificates = &v1alpha1.ServingCertificatesSpec{
		Ingress: &v1alpha1.IngressServingCertificateSpec{
			DefaultCertificateRef: v1alpha1.SecretRef{Name: "ingress-tls"},
		},
	}

	secretsDir := t.TempDir()
	writeInstallerCoreSecrets(t, secretsDir)
	writeSelfSignedPair(t, secretsDir, "ingress-tls", v1alpha1.SelfSignedCertificateSpec{
		CommonName:   "wrong.example.test",
		DNSNames:     []string{"wrong.example.test"},
		ValidityDays: 1,
	})
	_, err = render.ResolveInstaller(t.TempDir(), secretsDir, state)
	if err == nil {
		t.Fatal("expected ingress SAN mismatch error")
	}
	if !strings.Contains(err.Error(), "does not cover ingress wildcard") {
		t.Fatalf("error %q missing ingress coverage detail", err)
	}
}

func writeInstallerCoreSecrets(t *testing.T, dir string) {
	t.Helper()
	for name, content := range map[string]string{
		"pull":              `{"auths":{"quay.io":{"auth":"dXNlcjpwYXNz"}}}`,
		"ssh":               "ssh-rsa AAAA test\n",
		"proxy-credentials": "proxy:secret\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func writeSelfSignedPair(t *testing.T, dir, name string, spec v1alpha1.SelfSignedCertificateSpec) ([]byte, []byte) {
	t.Helper()
	cert, key, err := testSelfSignedCertificatePEM(spec)
	if err != nil {
		t.Fatalf("generate %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), cert, 0o600); err != nil {
		t.Fatalf("write %s cert: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".key"), key, 0o600); err != nil {
		t.Fatalf("write %s key: %v", name, err)
	}
	return cert, key
}

func testSelfSignedCertificatePEM(source v1alpha1.SelfSignedCertificateSpec) ([]byte, []byte, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, err
	}
	dnsNames := append([]string(nil), source.DNSNames...)
	var ipAddresses []net.IP
	for _, value := range source.IPAddresses {
		if ip := net.ParseIP(value); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		}
	}
	if len(dnsNames) == 0 && len(ipAddresses) == 0 && source.CommonName != "" {
		if ip := net.ParseIP(source.CommonName); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		} else {
			dnsNames = append(dnsNames, source.CommonName)
		}
	}
	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: source.CommonName,
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(0, 0, source.ValidityDays),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	var certBuf bytes.Buffer
	if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return nil, nil, err
	}
	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		return nil, nil, err
	}
	return certBuf.Bytes(), keyBuf.Bytes(), nil
}

func readManifest(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return doc
}
