package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/state/scaffold"
)

// TestWorkspaceUnknownProvider asserts the dispatch error names every
// known provider so the user sees the supported set instead of just
// "unknown".
func TestWorkspaceUnknownProvider(t *testing.T) {
	_, err := scaffold.Workspace("c1", scaffold.Provider("not-a-provider"))
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
	for _, want := range scaffold.KnownProviders() {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should list known provider %q; got %q", want, err)
		}
	}
}

func TestApplySupportClassifiesScaffoldProviders(t *testing.T) {
	supported := []scaffold.Provider{
		scaffold.ProviderEmulatedBareMetal,
		scaffold.ProviderBareMetal,
		scaffold.ProviderKubeVirt,
		scaffold.ProviderVSphere,
	}
	for _, provider := range supported {
		if got := scaffold.ApplySupport(provider); !got.ApplySupported() {
			t.Fatalf("%s should be apply-supported, got %s: %s", provider, got.Status, got.Summary)
		}
	}
}

// TestWorkspaceProducesScaffoldFiles asserts the scaffolder emits one
// YAML file per object for every provider. Names are pinned so
// downstream init.go's `os.WriteFile` directory layout doesn't drift.
func TestWorkspaceProducesScaffoldFiles(t *testing.T) {
	defaultNames := []string{
		"environment.yaml", "secrets.yaml", "infra/providers/provider.yaml", "infra/networkconfigs/networks.yaml",
		"clusters/container/cluster-a/cluster.yaml", "clusters/container/cluster-a/cluster-machines.yaml",
	}
	baremetalNames := []string{
		"environment.yaml", "secrets.yaml", "infra/providers/provider.yaml", "infra/machines/bastion.yaml",
		"infra/networkconfigs/networks.yaml", "infra/components/artifact-server.yaml",
		"clusters/container/cluster-a/cluster.yaml", "clusters/container/cluster-a/cluster-machines.yaml",
	}
	emulatedNames := []string{
		"environment.yaml", "secrets.yaml", "infra/providers/provider.yaml", "infra/machines/bastion.yaml",
		"infra/networkconfigs/networks.yaml", "infra/components/load-balancer.yaml",
		"infra/components/name-resolution.yaml", "infra/components/ntp-server.yaml",
		"clusters/container/cluster-a/cluster.yaml", "clusters/container/cluster-a/cluster-machines.yaml",
	}
	for _, p := range scaffold.KnownProviders() {
		t.Run(p, func(t *testing.T) {
			wantNames := defaultNames
			switch scaffold.Provider(p) {
			case scaffold.ProviderBareMetal:
				wantNames = baremetalNames
			case scaffold.ProviderEmulatedBareMetal:
				wantNames = emulatedNames
			}
			files, err := scaffold.Workspace("cluster-a", scaffold.Provider(p))
			if err != nil {
				t.Fatalf("Workspace(%q): %v", p, err)
			}
			if len(files) != len(wantNames) {
				t.Fatalf("got %d files, want %d", len(files), len(wantNames))
			}
			for i, f := range files {
				if f.Name != wantNames[i] {
					t.Errorf("file[%d].Name = %q, want %q", i, f.Name, wantNames[i])
				}
				if f.Body == "" {
					t.Errorf("file[%d].Body for %s is empty", i, f.Name)
				}
			}
		})
	}
}

// TestWorkspacePassesValidator is the load-bearing test: every
// substrate's scaffolded output must round-trip through
// LoadNormalizeValidate without error. Catches schema drift between
// the scaffolder's templates and the actual API types — the exact
// gap that motivated this rewrite.
func TestWorkspacePassesValidator(t *testing.T) {
	for _, p := range scaffold.KnownProviders() {
		t.Run(p, func(t *testing.T) {
			files, err := scaffold.Workspace("test-cluster", scaffold.Provider(p))
			if err != nil {
				t.Fatalf("Workspace: %v", err)
			}
			dir := t.TempDir()
			for _, f := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f.Name)), 0o700); err != nil {
					t.Fatalf("mkdir %s: %v", f.Name, err)
				}
				if err := os.WriteFile(filepath.Join(dir, f.Name), []byte(f.Body), 0o600); err != nil {
					t.Fatalf("write %s: %v", f.Name, err)
				}
			}
			if _, err := desiredstate.LoadNormalizeValidate([]string{dir}); err != nil {
				t.Fatalf("scaffolded state failed validation for provider %q: %v", p, err)
			}
		})
	}
}

// TestWorkspaceInterpolatesClusterName confirms the cluster name lands
// in metadata.name everywhere the templates reference it. A regression
// where a substrate forgot to substitute would scaffold a workspace
// that fails the unique-name check.
func TestWorkspaceInterpolatesClusterName(t *testing.T) {
	files, err := scaffold.Workspace("my-cluster", scaffold.ProviderEmulatedBareMetal)
	if err != nil {
		t.Fatal(err)
	}
	expectations := map[string][]string{
		"environment.yaml": {"name: my-cluster"},
		"clusters/container/my-cluster/cluster-machines.yaml": {"name: my-cluster-master-0", "providerRef: my-cluster-libvirt"},
		"infra/networkconfigs/networks.yaml":                  {"name: my-cluster-bridge"},
		"infra/providers/provider.yaml":                       {"name: my-cluster-libvirt"},
		"clusters/container/my-cluster/cluster.yaml":          {"name: my-cluster", "machineRef: my-cluster-master-0"},
	}
	for _, f := range files {
		wants, ok := expectations[f.Name]
		if !ok {
			continue
		}
		for _, want := range wants {
			if !strings.Contains(f.Body, want) {
				t.Errorf("%s missing expected interpolation %q\nbody:\n%s", f.Name, want, f.Body)
			}
		}
	}
}

func TestEmulatedBareMetalScaffoldOmitsUnselectedProxy(t *testing.T) {
	files, err := scaffold.Workspace("my-cluster", scaffold.ProviderEmulatedBareMetal)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f.Body, "proxy-credentials") ||
			strings.Contains(f.Body, "name: proxy") ||
			strings.Contains(f.Body, "proxyFor:") {
			t.Fatalf("%s contains unselected proxy material:\n%s", f.Name, f.Body)
		}
	}
}

func TestWorkspaceDoesNotRenderFlowStyleCollections(t *testing.T) {
	for _, p := range scaffold.KnownProviders() {
		t.Run(p, func(t *testing.T) {
			files, err := scaffold.Workspace("no-flow-maps", scaffold.Provider(p))
			if err != nil {
				t.Fatalf("workspace: %v", err)
			}
			for _, f := range files {
				if strings.ContainsAny(f.Body, "{}[]") {
					t.Fatalf("%s renders flow-style collection syntax:\n%s", f.Name, f.Body)
				}
			}
		})
	}
}

func TestWorkspaceUsesContextSecretDeclaration(t *testing.T) {
	for _, p := range scaffold.KnownProviders() {
		t.Run(p, func(t *testing.T) {
			files, err := scaffold.Workspace("cluster-a", scaffold.Provider(p))
			if err != nil {
				t.Fatalf("Workspace(%q): %v", p, err)
			}
			var secretsBody string
			for _, f := range files {
				if f.Name == "secrets.yaml" {
					secretsBody = f.Body
				}
				for _, forbidden := range []string{"file: ./pull-secret.json", "file: ./vcenter-credentials"} {
					if strings.Contains(f.Body, forbidden) {
						t.Fatalf("%s contains repo-local secret example %q:\n%s", f.Name, forbidden, f.Body)
					}
				}
			}
			if secretsBody == "" {
				t.Fatalf("no secrets.yaml generated for %q", p)
			}
			if !strings.Contains(secretsBody, "name: openshift-pull-secret") {
				t.Fatalf("secrets.yaml missing pull secret declaration:\n%s", secretsBody)
			}
			if strings.Contains(secretsBody, "path: ../secrets/openshift-pull-secret") {
				t.Fatalf("pull secret should be declared as context-local material:\n%s", secretsBody)
			}
		})
	}
}

func TestWorkspaceOmitsDeterministicDefaults(t *testing.T) {
	for _, p := range scaffold.KnownProviders() {
		t.Run(p, func(t *testing.T) {
			files, err := scaffold.Workspace("compact", scaffold.Provider(p))
			if err != nil {
				t.Fatalf("workspace: %v", err)
			}
			for _, f := range files {
				for _, forbidden := range []string{
					"type: ed25519",
					"username: admin",
					"type: openshift",
					"method: agent",
					"mode: connected",
					"protocol: redfish",
					"port: 8000",
					"vMediaPort: 8001",
					"port: 8443",
					"default: true",
				} {
					if strings.Contains(f.Body, forbidden) {
						t.Fatalf("%s contains deterministic default %q:\n%s", f.Name, forbidden, f.Body)
					}
				}
			}
		})
	}
}

func TestSubstratesCarryDistinctNetworkAttachments(t *testing.T) {
	want := map[scaffold.Provider]string{
		scaffold.ProviderEmulatedBareMetal: "libvirt:",
		scaffold.ProviderBareMetal:         "baremetal:",
		scaffold.ProviderVSphere:           "vsphere:",
		scaffold.ProviderKubeVirt:          "kubevirt:",
	}
	for p, fragment := range want {
		t.Run(string(p), func(t *testing.T) {
			s, ok := scaffold.Substrates[p]
			if !ok {
				t.Fatalf("substrate %q missing", p)
			}
			if !strings.Contains(s.ProviderCapabilities, fragment) {
				t.Errorf("%s network attachment should mention %q, got: %s", p, fragment, s.ProviderCapabilities)
			}
		})
	}
}
