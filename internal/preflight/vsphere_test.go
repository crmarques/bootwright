package preflight

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
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
			Spec:       v1alpha1.EnvironmentSpec{},
		}},
		Secrets: []v1alpha1.Secret{{
			Metadata:   v1alpha1.Metadata{Name: "vcenter-credentials"},
			SourcePath: filepath.Join(sourceDir, "environment.yaml"),
			Spec: v1alpha1.SecretSpec{
				Type:   v1alpha1.SecretTypeUsernamePassword,
				Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: "vcenter-credentials"}},
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

func TestVSphereVCenterSessionCheck(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	const sessionID = "session-token-42"
	respond := func(status int, recorded *[]*http.Request) func(req *http.Request, insecure bool) (*http.Response, error) {
		return func(req *http.Request, insecure bool) (*http.Response, error) {
			*recorded = append(*recorded, req)
			if req.URL.String() != "https://vcenter.example.test:443/api/session" {
				t.Fatalf("unexpected probe request: %s %s", req.Method, req.URL)
			}
			if !insecure {
				t.Fatal("probe must honor disableCertificateVerification")
			}
			switch req.Method {
			case http.MethodPost:
				user, pass, ok := req.BasicAuth()
				if !ok || user != "administrator@vsphere.local" || pass != "vc-password" {
					t.Fatalf("probe basic auth = %q/%q", user, pass)
				}
				header := http.Header{}
				header.Set("vmware-api-session-id", sessionID)
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(""))}, nil
			case http.MethodDelete:
				if got := req.Header.Get("vmware-api-session-id"); got != sessionID {
					t.Fatalf("session release header = %q, want %q", got, sessionID)
				}
				return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader(""))}, nil
			default:
				t.Fatalf("unexpected probe method: %s", req.Method)
				return nil, nil
			}
		}
	}

	var okRequests []*http.Request
	checks := vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: respond(http.StatusCreated, &okRequests)})
	if session, ok := findCheckByName(checks, "vcenter.example.test vCenter session"); !ok || session.Status != StatusOK {
		t.Fatalf("session check with 201 = %+v, want an OK session check", checks)
	}
	if len(okRequests) != 2 || okRequests[0].Method != http.MethodPost || okRequests[1].Method != http.MethodDelete {
		t.Fatalf("probe request sequence = %v, want POST then DELETE", okRequests)
	}

	var failRequests []*http.Request
	checks = vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: respond(http.StatusUnauthorized, &failRequests)})
	if session, ok := findCheckByName(checks, "vcenter.example.test vCenter session"); !ok || session.Status != StatusFail || !strings.Contains(session.Impact, "rejected the declared credentials") {
		t.Fatalf("session check with 401 = %+v, want a credentials failure", checks)
	}
	if len(failRequests) != 1 || failRequests[0].Method != http.MethodPost {
		t.Fatalf("failed probe must not release a session: %v", failRequests)
	}
}

func TestVSphereVCenterSessionCheckReadsContextStoreMaterial(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	state.Secrets[0].Spec.Source = v1alpha1.SecretSource{
		Generated: &v1alpha1.SecretGeneratedSource{Username: "administrator@vsphere.local"},
	}
	store := secretstore.NewContextStore("test", secretsDir)
	if err := store.Write(secretstore.MaterialKey{Name: "vcenter-credentials", Role: secretstore.MaterialPrimary}, []byte("administrator@vsphere.local:vc-password\n")); err != nil {
		t.Fatalf("write context secret: %v", err)
	}
	checks := vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		user, pass, ok := req.BasicAuth()
		if !ok || user != "administrator@vsphere.local" || pass != "vc-password" {
			t.Fatalf("probe basic auth = %q/%q", user, pass)
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
	}})
	if session, ok := findCheckByName(checks, "vcenter.example.test vCenter session"); !ok || session.Status != StatusOK {
		t.Fatalf("context-store session check = %+v, want an OK session check", checks)
	}
}

func TestVSphereVCenterSessionCheckWarnsWithoutMaterial(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	state.Secrets[0].Spec.Source = v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: "missing-file"}}
	checks := vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		t.Fatal("probe must not run without credential material")
		return nil, nil
	}})
	if session, ok := findCheckByName(checks, "vcenter.example.test vCenter session"); !ok || session.Status != StatusWarn {
		t.Fatalf("session check without material = %+v, want a WARN session check", checks)
	}
}

func findCheckByName(checks []Check, name string) (Check, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

func TestVSphereVCenterInsecureTLSWarns(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	checks := vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
	}})
	warn, ok := findCheckByName(checks, "vcenter.example.test vCenter TLS")
	if !ok || warn.Status != StatusWarn || !strings.Contains(warn.Impact, "unverified TLS") {
		t.Fatalf("insecure vCenter probe = %+v, want a WARN naming unverified TLS", checks)
	}

	state.InfraProviders[0].Spec.VSphere.VCenters[0].DisableCertificateVerification = false
	checks = vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
	}})
	if _, ok := findCheckByName(checks, "vcenter.example.test vCenter TLS"); ok {
		t.Fatalf("verifying vCenter probe must not emit a TLS warning: %+v", checks)
	}
}

func TestVSphereVCenterDedupesByCredentials(t *testing.T) {
	state, secretsDir := vsphereCheckTestState(t)
	state.Secrets = append(state.Secrets, v1alpha1.Secret{
		Metadata:   v1alpha1.Metadata{Name: "vcenter-credentials-2"},
		SourcePath: state.Environments[0].SourcePath,
		Spec: v1alpha1.SecretSpec{
			Type:   v1alpha1.SecretTypeUsernamePassword,
			Source: v1alpha1.SecretSource{File: &v1alpha1.SecretFileSource{Path: "vcenter-credentials"}},
		},
	})
	state.InfraProviders = append(state.InfraProviders, v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "lab-vsphere-2"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerVSphere,
			VSphere: &v1alpha1.InfraProviderVSphere{
				VCenters: []v1alpha1.VSphereVCenter{{
					Server:                         "vcenter.example.test",
					Datacenters:                    []string{"dc1"},
					CredentialsRef:                 v1alpha1.SecretRef{Name: "vcenter-credentials-2"},
					DisableCertificateVerification: true,
				}},
			},
		},
	})
	var probes atomic.Int64
	checks := vsphereVCenterChecks(state, nil, "test", secretsDir, Deps{HTTPDo: func(req *http.Request, insecure bool) (*http.Response, error) {
		if req.Method == http.MethodPost {
			probes.Add(1)
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: io.NopCloser(strings.NewReader(""))}, nil
	}})
	if probes.Load() != 2 {
		t.Fatalf("session POST probes = %d, want 2 (one per distinct credential): %+v", probes.Load(), checks)
	}
}
