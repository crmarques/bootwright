package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/render"
	secretstore "github.com/crmarques/bootwright/internal/secrets"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// TestToolInputsPortableRendersSecretPlaceholders renders the context-free
// portable bundle and asserts every secret reference is a {{ secret <name> }}
// token, with no real secret material, no context-secrets-dir path, no leftover
// "<bootwright-...>" placeholder, and no leak of the placeholder sentinel.
func TestToolInputsPortableRendersSecretPlaceholders(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	outputDir := t.TempDir()
	result, err := render.ToolInputsPortable(outputDir, state)
	if err != nil {
		t.Fatalf("ToolInputsPortable: %v", err)
	}

	installConfig := readFile(t, result.InstallerAssets[0].InstallConfigPath)
	if !strings.Contains(installConfig, "pullSecret: '{{ secret ") {
		t.Errorf("install-config pullSecret is not a portable token:\n%s", installConfig)
	}
	if !strings.Contains(installConfig, ".ssh-public }}") {
		t.Errorf("install-config sshKey is not a portable ssh-public token:\n%s", installConfig)
	}

	vars := readFile(t, result.VarsPath)
	if !strings.Contains(vars, "{{ secret ") || !strings.Contains(vars, ".ssh-private }}") {
		t.Errorf("vars.yaml did not carry node SSH private-key token:\n%s", vars)
	}

	walkRendered(t, outputDir, func(path, content string) {
		for _, bad := range portableForbidden {
			if strings.Contains(content, bad) {
				t.Errorf("%s leaked %q", path, bad)
			}
		}
	})
}

// portableForbidden is the whole-tree leak blocklist a portable bundle must
// never contain: the NUL-wrapped sentinel, the context "<bootwright-...>"
// placeholder dialect, real PEM material, and the context-only Ansible
// extra-var {{ bootwright_clusters_dir }} (a path no context-free consumer can
// resolve).
var portableForbidden = []string{
	secretstore.PlaceholderSecretsDir,
	"<bootwright-",
	"-----BEGIN",
	"bootwright_clusters_dir",
}

// TestToolInputsPortableTokenizesNestedKubeVirtStorageEntitlements renders a
// rich topology (nested KubeVirt-on-managed-host clusters + managed Ceph + ODF
// + RHSM entitlements) and asserts every secret-bearing surface tokenizes and
// nothing context-bound leaks.
func TestToolInputsPortableTokenizesNestedKubeVirtStorageEntitlements(t *testing.T) {
	fixture := filepath.Join("..", "..", "examples", "baremetal-redfish-multidc-virtualized-odf-ceph")
	state, err := desiredstate.LoadNormalizeValidate([]string{fixture})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}

	outputDir := t.TempDir()
	result, err := render.ToolInputsPortable(outputDir, state)
	if err != nil {
		t.Fatalf("ToolInputsPortable: %v", err)
	}

	vars := readFile(t, result.VarsPath)
	for _, want := range []string{
		"-kubeconfig }}",                // KubeVirt host-cluster kubeconfig (context path -> token)
		".ssh-private }}",               // storage cluster SSH private key
		"organizationPath: '{{ secret ", // RHSM entitlement
	} {
		if !strings.Contains(vars, want) {
			t.Errorf("vars.yaml missing portable token %q", want)
		}
	}

	walkRendered(t, outputDir, func(path, content string) {
		for _, bad := range portableForbidden {
			if strings.Contains(content, bad) {
				t.Errorf("%s leaked %q", path, bad)
			}
		}
	})
}

// TestToolInputsPortableFailsFastOnDisconnectedMirror pins that a cluster whose
// install-config needs disconnected mirror-registry auth (which has no portable
// token form) is rejected up front rather than rendered into a silently
// incomplete bundle.
func TestToolInputsPortableFailsFastOnDisconnectedMirror(t *testing.T) {
	fixture := filepath.Join("..", "..", "examples", "sno-libvirt-redfish-disconnected-services")
	state, err := desiredstate.LoadNormalizeValidate([]string{fixture})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	_, err = render.ToolInputsPortable(t.TempDir(), state)
	if err == nil {
		t.Fatal("expected portable render to fail fast on disconnected mirror credentials")
	}
	if !strings.Contains(err.Error(), "mirror-registry credentials") {
		t.Fatalf("error %q does not name the unsupported mirror credentials", err)
	}
}

// TestToolInputsPortableOnTightensModes pins that the portable render still
// flows through the same security seam: every directory 0700, every file 0600.
func TestToolInputsPortableOnTightensModes(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join(fixtureRoot, "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	fs := &recordingFS{}
	if _, err := render.ToolInputsPortableOn(fs, "/synthetic/out", state); err != nil {
		t.Fatalf("ToolInputsPortableOn: %v", err)
	}
	if len(fs.mkdirs) == 0 || len(fs.writes) == 0 {
		t.Fatal("ToolInputsPortableOn made no filesystem calls")
	}
	for _, m := range fs.mkdirs {
		if m.mode != wantLocalDirMode {
			t.Errorf("MkdirAll(%s) mode = %o want %o", m.path, m.mode, wantLocalDirMode)
		}
	}
	for _, w := range fs.writes {
		if w.mode != wantLocalFileMode {
			t.Errorf("WriteAtomic(%s) mode = %o want %o", w.path, w.mode, wantLocalFileMode)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func walkRendered(t *testing.T, root string, fn func(path, content string)) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}
