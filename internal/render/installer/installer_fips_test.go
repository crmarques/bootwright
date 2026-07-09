package installer_test

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/render/installer"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

func TestInstallerConfigRendersFIPS(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if len(state.ContainerClusters) == 0 {
		t.Fatal("fixture is missing a ContainerCluster")
	}
	ocp := state.ContainerClusters[0]

	cfg, err := installer.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig (disabled): %v", err)
	}
	if _, ok := cfg["fips"]; ok {
		t.Fatalf("install-config carries a fips key with FIPS disabled: %v", cfg["fips"])
	}

	ocp.Spec.Security.FIPS.Enabled = true
	cfg, err = installer.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig (enabled): %v", err)
	}
	if got, ok := cfg["fips"].(bool); !ok || !got {
		t.Fatalf("install-config fips = %v (%T), want true", cfg["fips"], cfg["fips"])
	}
}
