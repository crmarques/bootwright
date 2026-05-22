package scaffold_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/scaffold"
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
	}
	for _, provider := range supported {
		if got := scaffold.ApplySupport(provider); !got.ApplySupported() {
			t.Fatalf("%s should be apply-supported, got %s: %s", provider, got.Status, got.Summary)
		}
	}

	scaffoldOnly := []scaffold.Provider{
		scaffold.ProviderVSphere,
		scaffold.ProviderKubeVirt,
	}
	for _, provider := range scaffoldOnly {
		if got := scaffold.ApplySupport(provider); got.ApplySupported() {
			t.Fatalf("%s should be schema/scaffold-only, got %s", provider, got.Status)
		}
	}
}

// TestWorkspaceProducesScaffoldFiles asserts the scaffolder emits one
// YAML file per object for every provider. Names are pinned so
// downstream init.go's `os.WriteFile` directory layout doesn't drift.
func TestWorkspaceProducesScaffoldFiles(t *testing.T) {
	wantNames := []string{
		"environment.yaml", "hosts.yaml", "networks.yaml", "provider.yaml",
		"clusterinfra.yaml", "containercluster.yaml",
	}
	for _, p := range scaffold.KnownProviders() {
		t.Run(p, func(t *testing.T) {
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
		"environment.yaml":      {"name: my-cluster"},
		"networks.yaml":         {"name: my-cluster-bridge"},
		"provider.yaml":         {"name: my-cluster-libvirt"},
		"clusterinfra.yaml":     {"name: my-cluster", "provider: my-cluster-libvirt"},
		"containercluster.yaml": {"name: my-cluster"},
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
			for _, f := range files {
				if f.Name != "environment.yaml" {
					continue
				}
				for _, forbidden := range []string{"file: ./pull-secret.json", "file: ./vcenter-credentials"} {
					if strings.Contains(f.Body, forbidden) {
						t.Fatalf("%s contains repo-local secret example %q:\n%s", f.Name, forbidden, f.Body)
					}
				}
				if !strings.Contains(f.Body, "    openshift-pull-secret:\n") {
					t.Fatalf("%s missing context pull secret declaration:\n%s", f.Name, f.Body)
				}
				if strings.Contains(f.Body, "file: ../secrets/openshift-pull-secret") {
					t.Fatalf("%s should declare pull secret as context-local material:\n%s", f.Name, f.Body)
				}
			}
		})
	}
}

// TestSubstratesCarryDistinctConnectivity verifies the Substrates map
// hasn't accidentally cross-wired two substrates to the same
// connectivity arm. The whole point of having four substrates is that
// each emits its own connectivity block.
func TestSubstratesCarryDistinctConnectivity(t *testing.T) {
	want := map[scaffold.Provider]string{
		scaffold.ProviderEmulatedBareMetal: "libvirt:",
		scaffold.ProviderBareMetal:         "physical:",
		scaffold.ProviderVSphere:           "vsphere:",
		scaffold.ProviderKubeVirt:          "kubevirt:",
	}
	for p, fragment := range want {
		t.Run(string(p), func(t *testing.T) {
			s, ok := scaffold.Substrates[p]
			if !ok {
				t.Fatalf("substrate %q missing", p)
			}
			if !strings.Contains(s.NetworkConnectivity, fragment) {
				t.Errorf("%s connectivity should mention %q, got: %s", p, fragment, s.NetworkConnectivity)
			}
		})
	}
}
