package render_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/provisioning/render"
)

// alwaysOwnedInstallConfigKeys are the install-config keys Bootwright
// writes on EVERY cluster regardless of install mode or fixture. Keys
// listed in OwnedFields().InstallConfigKeys that are conditional
// (additionalTrustBundle: disconnected only; imageDigestSources:
// disconnected or operator-declared mirror) live in conditionalOwned
// below.
var alwaysOwnedInstallConfigKeys = []string{
	"apiVersion",
	"metadata",
	"baseDomain",
	"pullSecret",
	"sshKey",
	"controlPlane",
	"compute",
}

// conditionalInstallConfigKeys are owned keys the renderer emits only
// in specific install modes. Listing them here documents the case
// instead of silently dropping them from the bridge check.
var conditionalInstallConfigKeys = map[string]string{
	"additionalTrustBundle":       "set only when ContainerCluster.spec.install.mode=disconnected",
	"additionalTrustBundlePolicy": "set only when additionalTrustBundle is set",
	"imageDigestSources":          "set only when Environment.spec.registries.mirror is declared (disconnected mode)",
	"proxy":                       "set only when Environment.spec.proxy.useFor.clusterInstall permits an external or managed proxy",
}

// alwaysOwnedAgentConfigKeys are the agent-config keys Bootwright
// always writes. minimalISO and bootArtifactsBaseURL fire only in
// disconnected mode (see disconnectedBootArtifactsConfig).
var alwaysOwnedAgentConfigKeys = []string{
	"apiVersion",
	"kind",
	"metadata",
	"rendezvousIP",
	"hosts",
}

// TestOwnedInstallConfigKeysAllHaveWriters bridges
// api/v1alpha1.OwnedFields() to the renderer: every key listed in
// OwnedFields().InstallConfigKeys must either be written by the
// renderer on the canonical good fixture OR be documented here as
// conditionally owned. The test fails if the registry gains a new key
// without a renderer that emits it (the validator denylist would then
// reject overrides for a field the renderer never set, which is wrong).
// It also fails if the renderer starts writing a key not in the
// registry (override hatch silently re-opens for it).
func TestOwnedInstallConfigKeysAllHaveWriters(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "desiredstate", "testdata", "good", "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if len(state.ContainerClusters) == 0 {
		t.Fatal("fixture is missing a ContainerCluster")
	}
	ocp := state.ContainerClusters[0]
	cfg, err := render.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}

	rendered := keys(cfg)
	owned := v1alpha1.OwnedFields().InstallConfigKeys

	// Every always-owned key MUST appear in the rendered config.
	for _, k := range alwaysOwnedInstallConfigKeys {
		if !slices.Contains(rendered, k) {
			t.Errorf("installer-config missing always-owned key %q (rendered: %v)", k, rendered)
		}
	}
	// Every owned key in the registry MUST be either always-written or
	// documented as conditional. This catches a future registry entry
	// landing without a renderer that emits it.
	for _, k := range owned {
		if slices.Contains(alwaysOwnedInstallConfigKeys, k) {
			continue
		}
		if _, ok := conditionalInstallConfigKeys[k]; ok {
			continue
		}
		t.Errorf("OwnedFields() declares key %q but the renderer neither always emits it nor documents it as conditional in conditionalInstallConfigKeys", k)
	}
	// Every rendered top-level key that is strictly Bootwright-derived
	// MUST be in OwnedFields(). Keys the user may legitimately extend
	// at the same level (networking, platform) live outside the registry by design
	// and are listed in userExtensibleKeys below.
	userExtensibleKeys := []string{
		"networking", // sibling fields remain extensible; Bootwright owns declared networking paths
		"platform",   // sibling fields remain extensible; Bootwright owns declared platform paths
	}
	for _, k := range rendered {
		if slices.Contains(owned, k) || slices.Contains(userExtensibleKeys, k) {
			continue
		}
		t.Errorf("renderer writes install-config key %q that is neither in OwnedFields() nor declared user-extensible — add it to OwnedFields() (closes the override hatch) or to userExtensibleKeys with a reason", k)
	}
}

// TestOwnedAgentConfigKeysAllHaveWriters does the same bridge for the
// agent-config keys, against the same fixture.
func TestOwnedAgentConfigKeysAllHaveWriters(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "desiredstate", "testdata", "good", "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if len(state.ContainerClusters) == 0 {
		t.Fatal("fixture is missing a ContainerCluster")
	}
	ocp := state.ContainerClusters[0]
	cfg, err := render.AgentConfig(state, ocp)
	if err != nil {
		t.Fatalf("AgentConfig: %v", err)
	}

	rendered := keys(cfg)
	owned := v1alpha1.OwnedFields().AgentConfigKeys

	for _, k := range alwaysOwnedAgentConfigKeys {
		if !slices.Contains(rendered, k) {
			t.Errorf("agent-config missing always-owned key %q (rendered: %v)", k, rendered)
		}
	}
	conditional := map[string]string{
		"minimalISO":           "set only when ContainerCluster.spec.install.mode=disconnected",
		"bootArtifactsBaseURL": "set only when ContainerCluster.spec.install.mode=disconnected",
		"additionalNTPSources": "set only when Environment.spec.ntpSources is declared",
	}
	for _, k := range owned {
		if slices.Contains(alwaysOwnedAgentConfigKeys, k) {
			continue
		}
		if _, ok := conditional[k]; ok {
			continue
		}
		t.Errorf("OwnedFields() declares agent-config key %q but the renderer neither always emits it nor documents it as conditional", k)
	}
	for _, k := range rendered {
		if !slices.Contains(owned, k) {
			t.Errorf("renderer writes agent-config key %q not in OwnedFields() — add it to close the override hatch", k)
		}
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
