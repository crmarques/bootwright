package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/render"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

func TestInfraComponentSharedServiceSelectionDigestBindsExactRenderedInput(t *testing.T) {
	seen := map[string]bool{}
	for _, fixture := range []string{
		filepath.Join("..", "..", "test", "e2e", "001-sno-libvirt"),
		filepath.Join("..", "..", "test", "e2e", "003-3nodes-libvirt"),
		filepath.Join("..", "..", "examples", "sno-libvirt-redfish-disconnected-services"),
	} {
		state, err := desiredstate.LoadNormalizeValidate([]string{fixture})
		if err != nil {
			t.Fatalf("load fixture %s: %v", fixture, err)
		}
		for _, service := range stategraph.ResolveMachineServices(state).Services {
			if !service.IsInfraComponentService() {
				continue
			}
			selection, ok := render.InfraComponentServiceSelection(state, service)
			if !ok {
				t.Fatalf("render exact selection for %+v", service.Identity)
			}
			digest := infraComponentSharedServiceSelectionDigest(state, service)
			if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
				t.Fatalf("selection digest for %s = %q", service.Identity.Kind, digest)
			}
			changed := make(map[string]any, len(selection)+1)
			for key, value := range selection {
				changed[key] = value
			}
			changed["futureHostMutation"] = true
			if changedDigest := infraComponentSharedServiceSelectionDigestForRendered(service.Identity.Kind, service.MachineRef, changed); changedDigest == digest {
				t.Fatalf("changed exact rendered %s input retained selection digest %q", service.Identity.Kind, digest)
			}
			seen[service.Identity.Kind] = true
		}
	}
	if len(seen) != 6 {
		t.Fatalf("fixture exercised %d infra-component kinds, want six: %v", len(seen), seen)
	}
}

func TestSelectedInfraComponentServiceRefsCarrySelectionDigests(t *testing.T) {
	state, err := desiredstate.LoadNormalizeValidate([]string{filepath.Join("..", "..", "examples", safetyBaselineAdvanced)})
	if err != nil {
		t.Fatalf("load advanced fixture: %v", err)
	}
	refs := selectedInfraComponentServiceRefs(state, false, false, nil)
	if len(refs) == 0 {
		t.Fatal("advanced fixture has no infra-component refs")
	}
	for _, ref := range refs {
		if len(ref.SelectionDigests) != 1 || len(ref.SelectionDigests[0]) != 71 || !strings.HasPrefix(ref.SelectionDigests[0], "sha256:") {
			t.Fatalf("infra-component ref lacks exact selection digest: %+v", ref)
		}
	}
}

func TestInfraComponentSelectionDigestUsesCrossLanguageCanonicalJSON(t *testing.T) {
	component := map[string]any{
		"kind":         "proxy",
		"providerName": "provider-a",
		"name":         "proxy-a",
		"machineRef":   "bastion",
		"applyRole":    "bootwright.core.infra_component_proxy_squid",
		"destroyRole":  "bootwright.core.infra_component_proxy_squid",
		"proxyURL":     "http://proxy/?a=1&b=2",
	}
	const want = "sha256:cefc3a3be08585d585698752ef8b3207e7ebfb7706fb34a1b1cc2445129a8069"
	if got := infraComponentSharedServiceSelectionDigestForRendered("proxy", "bastion", component); got != want {
		t.Fatalf("selection digest = %q, want cross-language digest %q", got, want)
	}
}
