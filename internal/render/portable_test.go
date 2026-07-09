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

var portableForbidden = []string{
	secretstore.PlaceholderSecretsDir,
	"<bootwright-",
	"-----BEGIN",
	"bootwright_clusters_dir",
}

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
		"-kubeconfig }}",
		".ssh-private }}",
		"organizationPath: '{{ secret ",
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
