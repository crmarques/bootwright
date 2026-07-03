package installer_test

import (
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/render/installer"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
)

// TestInstallerConfigRendersFIPS asserts spec.security.fips.enabled drives the
// top-level install-config fips key: absent when disabled, true when enabled.
// That is the sole OS-FIPS mechanism for OCP — the agent installer lays down
// RHCOS in FIPS mode from this field, so no separate node OS gate exists.
func TestInstallerConfigRendersFIPS(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if len(state.ContainerClusters) == 0 {
		t.Fatal("fixture is missing a ContainerCluster")
	}
	ocp := state.ContainerClusters[0]

	// Fixture default: FIPS unset, so no fips key at all.
	cfg, err := installer.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig (disabled): %v", err)
	}
	if _, ok := cfg["fips"]; ok {
		t.Fatalf("install-config carries a fips key with FIPS disabled: %v", cfg["fips"])
	}

	// Enabled: fips: true.
	ocp.Spec.Security.FIPS.Enabled = true
	cfg, err = installer.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig (enabled): %v", err)
	}
	if got, ok := cfg["fips"].(bool); !ok || !got {
		t.Fatalf("install-config fips = %v (%T), want true", cfg["fips"], cfg["fips"])
	}
}
