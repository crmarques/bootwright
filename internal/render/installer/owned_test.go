package installer_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render/installer"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
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
	"proxy":                       "set only when Environment.spec.proxyFor.containerClusterInstall selects an external or managed proxy",
}

var conditionalInstallConfigPaths = map[string]string{
	"networking.networkType":                 "set only when ContainerCluster.spec.networking.networkType is declared",
	"platform.baremetal.apiVIPs":             "set only when ClusterInstall.spec.platform.type renders baremetal",
	"platform.baremetal.ingressVIPs":         "set only when ClusterInstall.spec.platform.type renders baremetal",
	"platform.baremetal.loadBalancer":        "set only when ClusterInstall.spec.platform.type renders baremetal",
	"platform.baremetal.provisioningNetwork": "set only when ClusterInstall.spec.platform.type renders baremetal",
	"platform.external":                      "set only when ClusterInstall.spec.platform.type renders external",
	"platform.vsphere.apiVIPs":               "set only when ClusterInstall.spec.platform.type renders vsphere",
	"platform.vsphere.ingressVIPs":           "set only when ClusterInstall.spec.platform.type renders vsphere",
	"platform.vsphere.vcenters":              "set only when ClusterInstall.spec.platform.type renders vsphere",
	"platform.vsphere.failureDomains":        "set only when ClusterInstall.spec.platform.type renders vsphere",
	"platform.vsphere.nodeNetworking":        "set only when ClusterInstall.spec.platform.type renders vsphere with node networking",
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

func TestOwnedInstallConfigPathsAllHaveWriters(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	ocp := state.ContainerClusters[0]
	cfg, err := installer.InstallerConfig(state, ocp)
	if err != nil {
		t.Fatalf("InstallerConfig: %v", err)
	}

	rendered := dottedPaths(cfg)
	owned := v1alpha1.OwnedFields().InstallConfigPaths
	for _, path := range owned {
		if slices.Contains(rendered, path) {
			continue
		}
		if _, ok := conditionalInstallConfigPaths[path]; ok {
			continue
		}
		t.Errorf("OwnedFields() declares install-config path %q but the canonical fixture does not render it and conditionalInstallConfigPaths does not classify it", path)
	}
	for _, path := range []string{
		"networking.machineNetwork",
		"networking.clusterNetwork",
		"networking.serviceNetwork",
		"platform.none",
	} {
		if !slices.Contains(rendered, path) {
			t.Errorf("canonical fixture missing owned install-config path %q (rendered: %v)", path, rendered)
		}
		if !slices.Contains(owned, path) {
			t.Errorf("canonical fixture renders install-config path %q but OwnedFields() does not list it", path)
		}
	}
	for _, path := range rendered {
		if !strings.HasPrefix(path, "networking.") && !strings.HasPrefix(path, "platform.") {
			continue
		}
		if slices.Contains(owned, path) || ownedPathParentContains(owned, path) {
			continue
		}
		t.Errorf("renderer writes install-config path %q that is not covered by OwnedFields().InstallConfigPaths", path)
	}
}

// TestOwnedAgentConfigKeysAllHaveWriters does the same bridge for the
// agent-config keys, against the same fixture.
func TestOwnedAgentConfigKeysAllHaveWriters(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "state", "desired", "testdata", "good", "001-sno-libvirt")})
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	if len(state.ContainerClusters) == 0 {
		t.Fatal("fixture is missing a ContainerCluster")
	}
	ocp := state.ContainerClusters[0]
	cfg, err := installer.AgentConfig(state, ocp)
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
		"additionalNTPSources": "set only when Environment.spec.infraComponents.ntpSources resolves to installer sources",
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

func dottedPaths(m map[string]any) []string {
	seen := map[string]bool{}
	var walk func(prefix string, value any)
	walk = func(prefix string, value any) {
		if prefix != "" {
			seen[prefix] = true
		}
		child, ok := value.(map[string]any)
		if !ok {
			return
		}
		for k, v := range child {
			next := k
			if prefix != "" {
				next = prefix + "." + k
			}
			walk(next, v)
		}
	}
	for k, v := range m {
		walk(k, v)
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

func ownedPathParentContains(owned []string, path string) bool {
	for _, candidate := range owned {
		if strings.HasPrefix(path, candidate+".") {
			return true
		}
	}
	return false
}
