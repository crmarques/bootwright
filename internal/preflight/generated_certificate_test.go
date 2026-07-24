package preflight

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

func TestGeneratedSelfSignedDriftCheckReadsEncryptedMaterial(t *testing.T) {
	const contextName = "prd"
	secretsDir := t.TempDir()
	state := v1alpha1.State{
		Secrets: []v1alpha1.Secret{{
			Metadata: v1alpha1.Metadata{Name: "ceph-prd-01-rgw-dc1-tls"},
			Spec: v1alpha1.SecretSpec{
				Type: v1alpha1.SecretTypeTLSCertificate,
				Source: v1alpha1.SecretSource{Generated: &v1alpha1.SecretGeneratedSource{
					CommonName:   "rgw-dc1.storage.example.com",
					ValidityDays: 365,
				}},
			},
		}},
	}
	if _, err := secret.MaterializeForContext(contextName, secretsDir, state, secret.MaterializeOptions{Generated: true, Renew: true}); err != nil {
		t.Fatalf("MaterializeForContext: %v", err)
	}

	checks := generatedSelfSignedDriftChecks(state, contextName, secretsDir)
	if len(checks) != 1 || checks[0].Status != StatusOK {
		t.Fatalf("generated certificate checks = %+v, want one OK check", checks)
	}

	state.Secrets[0].Spec.Source.Generated.CommonName = "other.storage.example.com"
	checks = generatedSelfSignedDriftChecks(state, contextName, secretsDir)
	if len(checks) != 1 || checks[0].Status != StatusFail || !strings.Contains(checks[0].Evidence, "commonName drift") {
		t.Fatalf("drifted generated certificate checks = %+v, want commonName drift failure", checks)
	}
}
