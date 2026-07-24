package preflight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/secrets"
)

func rgwIngressTLSPreflightState() v1alpha1.State {
	generated := func(name string) v1alpha1.Secret {
		return v1alpha1.Secret{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.SecretSpec{
				Type:   v1alpha1.SecretTypeTLSCertificate,
				Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{}},
			},
		}
	}
	return v1alpha1.State{
		Secrets: []v1alpha1.Secret{
			generated("rgw-certificate"),
			generated("rgw-key"),
		},
		StorageObjectGateways: []v1alpha1.StorageObjectGateway{{
			Metadata: v1alpha1.Metadata{Name: "rgw"},
			Spec: v1alpha1.StorageObjectGatewaySpec{
				StorageClusterRef: v1alpha1.LocalObjectReference{Name: "ceph"},
				Ceph: v1alpha1.StorageObjectGatewayCephSpec{
					Ingresses: []v1alpha1.StorageObjectGatewayIngress{{
						Name: "ha",
						TLS: &v1alpha1.StorageObjectGatewayIngressTLS{
							CertificateRef: v1alpha1.LocalObjectReference{Name: "rgw-certificate"},
							KeyRef:         v1alpha1.LocalObjectReference{Name: "rgw-key"},
						},
					}},
				},
			},
		}},
	}
}

func TestRGWIngressTLSRefsAreRequiredStorageMaterial(t *testing.T) {
	requirements := collectStorageSecretRefRequirements(rgwIngressTLSPreflightState())
	if len(requirements) != 2 {
		t.Fatalf("requirements = %d, want certificate and key: %+v", len(requirements), requirements)
	}
	if requirements[0].refName != "rgw-certificate" || requirements[0].role != secret.MaterialPrimary {
		t.Fatalf("certificate requirement = %+v, want primary material", requirements[0])
	}
	if requirements[1].refName != "rgw-key" || requirements[1].role != secret.MaterialTLSKey {
		t.Fatalf("key requirement = %+v, want TLS key material", requirements[1])
	}
	for _, requirement := range requirements {
		if len(requirement.phases) != 1 || requirement.phases[0] != "base" {
			t.Fatalf("requirement phases = %v, want [base]", requirement.phases)
		}
		if requirement.owner.storageCluster != "ceph" {
			t.Fatalf("requirement owner = %+v, want storage cluster ceph", requirement.owner)
		}
	}
}

func TestRGWIngressTLSMissingGeneratedMaterialFailsPreflight(t *testing.T) {
	secretsDir := t.TempDir()
	checks := secretRefChecks(rgwIngressTLSPreflightState(), secretsDir, []Phase{{Name: "base"}}, Deps{StatPath: os.Stat})
	failures := map[string]Check{}
	for _, check := range checks {
		if check.Status == StatusFail && strings.Contains(check.Name, "StorageObjectGateway/rgw") {
			failures[check.Name] = check
		}
	}
	if len(failures) != 2 {
		t.Fatalf("RGW TLS failures = %d, want certificate and key: %+v", len(failures), failures)
	}
	certificate := failures["StorageObjectGateway/rgw spec.ceph.ingresses[0].tls.certificateRef tls.crt"]
	if want := filepath.Join(secretsDir, "rgw-certificate"); !strings.Contains(certificate.Evidence, want+" missing") {
		t.Fatalf("certificate evidence = %q, want missing path %s", certificate.Evidence, want)
	}
	key := failures["StorageObjectGateway/rgw spec.ceph.ingresses[0].tls.keyRef tls.key"]
	if want := filepath.Join(secretsDir, "rgw-key.key"); !strings.Contains(key.Evidence, want+" missing") {
		t.Fatalf("key evidence = %q, want missing path %s", key.Evidence, want)
	}
	for _, check := range []Check{certificate, key} {
		if check.Remediation != "bootwright secret generate" {
			t.Fatalf("%s remediation = %q, want bootwright secret generate", check.Name, check.Remediation)
		}
	}
}
