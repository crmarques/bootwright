package preflight

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func vsphereCheckTestState(t *testing.T) (v1alpha1.State, string) {
	t.Helper()
	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, "vcenter-credentials"), []byte("administrator@vsphere.local:vc-password\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	state := v1alpha1.State{
		Environments: []v1alpha1.Environment{{
			Metadata:   v1alpha1.Metadata{Name: "env"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.EnvironmentSpec{
				Secrets: map[string]v1alpha1.EnvironmentSecretSpec{
					"vcenter-credentials": {File: "vcenter-credentials"},
				},
			},
		}},
		InfraProviders: []v1alpha1.InfraProvider{{
			Metadata: v1alpha1.Metadata{Name: "lab-vsphere"},
			Spec: v1alpha1.InfraProviderSpec{
				Type: v1alpha1.ProvisionerVSphere,
				VSphere: &v1alpha1.InfraProviderVSphere{
					VCenters: []v1alpha1.VSphereVCenter{{
						Server:                         "vcenter.example.test",
						Datacenters:                    []string{"dc1"},
						CredentialsRef:                 v1alpha1.SecretRef{Name: "vcenter-credentials"},
						DisableCertificateVerification: true,
					}},
				},
			},
		}},
		Machines: []v1alpha1.Machine{{
			Metadata: v1alpha1.Metadata{Name: "master-0"},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{
					ProviderRef: v1alpha1.LocalObjectReference{Name: "lab-vsphere"},
					ProfileRef:  v1alpha1.LocalObjectReference{Name: "node"},
				},
			},
		}},
	}
	return state, t.TempDir()
}

// TestVSphereVCenterSessionCheck pins the live vCenter probe: a session
// login with the resolved user:password material, honoring the per-vCenter
// certificate-verification opt-out, classifying 2xx as OK and 401 as a
// credentials failure.
func TestVSphereVCenterSessionCheck(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	respond := func(status int) func(req *http.Request, insecure bool) (*http.Response, error) {
		return func(req *http.Request, insecure bool) (*http.Response, error) {
			if req.Method != http.MethodPost || req.URL.String() != "https://vcenter.example.test:443/api/session" {
				t.Fatalf("unexpected probe request: %s %s", req.Method, req.URL)
			}
			if !insecure {
				t.Fatal("probe must honor disableCertificateVerification")
			}
			user, pass, ok := req.BasicAuth()
			if !ok || user != "administrator@vsphere.local" || pass != "vc-password" {
				t.Fatalf("probe basic auth = %q/%q", user, pass)
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
	}

	checks := vsphereVCenterChecks(state, nil, secretsDir, Deps{HTTPDo: respond(http.StatusCreated)})
	if len(checks) != 1 || checks[0].Status != StatusOK {
		t.Fatalf("session check with 201 = %+v, want one OK check", checks)
	}

	checks = vsphereVCenterChecks(state, nil, secretsDir, Deps{HTTPDo: respond(http.StatusUnauthorized)})
	if len(checks) != 1 || checks[0].Status != StatusFail || !strings.Contains(checks[0].Impact, "rejected the declared credentials") {
		t.Fatalf("session check with 401 = %+v, want a credentials failure", checks)
	}
}

// TestVSphereVCenterSessionCheckWarnsWithoutMaterial pins the degraded
// path: unreadable credential material downgrades the probe to a warning
// because the secret-material checks already fail loudly.
func TestVSphereVCenterSessionCheckWarnsWithoutMaterial(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	state.Environments[0].Spec.Secrets["vcenter-credentials"] = v1alpha1.EnvironmentSecretSpec{File: "missing-file"}
	checks := vsphereVCenterChecks(state, nil, secretsDir, Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		t.Fatal("probe must not run without credential material")
		return nil, nil
	}})
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("session check without material = %+v, want one WARN check", checks)
	}
}
