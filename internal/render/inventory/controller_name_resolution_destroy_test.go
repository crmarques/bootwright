package inventory

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
)

const testControllerResolverOwnershipName = "resolver-ed1fb34838e8c117ca26833db903bbda88039ded4b62cbc26a6a147c5606df91"

func TestControllerNameResolutionDestroyTargetsUseRegistryContract(t *testing.T) {
	targets := controllerDestroyTargets(t, Vars(dnsRecordsState()))
	if len(targets) != 1 {
		t.Fatalf("destroy targets = %#v, want one desired target", targets)
	}
	target := targets[0]
	for key, want := range map[string]any{
		"kind":                     v1alpha1.ComponentSlotNameResolution,
		"providerName":             v1alpha1.KindInfraComponent,
		"name":                     "dns",
		"machineRef":               "host",
		"realisation":              v1alpha1.InfraComponentTypeDnsmasq,
		"destroyRole":              "bootwright.core.infra_component_name_resolution_dnsmasq",
		"infraComponentRecordName": "InfraComponent-dns",
		"recordBacked":             false,
		"valid":                    true,
	} {
		if got := target[key]; got != want {
			t.Fatalf("target[%q] = %#v, want %#v: %#v", key, got, want, target)
		}
	}
}

func TestControllerNameResolutionDestroyTargetsRecoverValidOrphanRecord(t *testing.T) {
	record := validControllerResolverRecord()
	vars := VarsWithPathOptionsAndOwnership(v1alpha1.State{}, PathOptions{}, []ownership.ResourceRecord{record})
	targets := controllerDestroyTargets(t, vars)
	if len(targets) != 1 {
		t.Fatalf("destroy targets = %#v, want one record-only target", targets)
	}
	target := targets[0]
	for key, want := range map[string]any{
		"destroyRole":                  "bootwright.core.infra_component_name_resolution_dnsmasq",
		"controllerOwnershipName":      testControllerResolverOwnershipName,
		"controllerOwnershipPath":      "resources/controller-name-resolver/" + testControllerResolverOwnershipName + ".json",
		"controllerResolverDropinPath": "/etc/systemd/resolved.conf.d/bootwright-" + testControllerResolverOwnershipName + ".conf",
		"infraComponentRecordName":     "InfraComponent-dns",
		"recordBacked":                 true,
		"valid":                        true,
	} {
		if got := target[key]; got != want {
			t.Fatalf("target[%q] = %#v, want %#v: %#v", key, got, want, target)
		}
	}
}

func TestControllerNameResolutionDestroyTargetsDeduplicateDesiredAndRecord(t *testing.T) {
	record := validControllerResolverRecord()
	vars := VarsWithPathOptionsAndOwnership(dnsRecordsState(), PathOptions{}, []ownership.ResourceRecord{record})
	targets := controllerDestroyTargets(t, vars)
	if len(targets) != 1 {
		t.Fatalf("desired plus matching record produced targets = %#v, want one", targets)
	}
	if targets[0]["recordBacked"] != true || targets[0]["valid"] != true {
		t.Fatalf("deduplicated target lost record evidence: %#v", targets[0])
	}
}

func TestControllerNameResolutionDestroyTargetsFailClosedOnInvalidRecord(t *testing.T) {
	record := validControllerResolverRecord()
	record.Paths = []string{"/etc/systemd/resolved.conf.d/operator-owned.conf"}
	record.Attributes["machineRef"] = "other-host"
	vars := VarsWithPathOptionsAndOwnership(dnsRecordsState(), PathOptions{}, []ownership.ResourceRecord{record})
	targets := controllerDestroyTargets(t, vars)
	if len(targets) != 1 {
		t.Fatalf("invalid matching record produced targets = %#v, want one fail-closed target", targets)
	}
	target := targets[0]
	if target["valid"] != false || target["destroyRole"] != "" {
		t.Fatalf("invalid ownership evidence retained executable dispatch: %#v", target)
	}
	errorText, _ := target["validationError"].(string)
	for _, want := range []string{"machineRef conflicts", "operator-owned.conf"} {
		if !strings.Contains(errorText, want) {
			t.Fatalf("validationError %q missing %q", errorText, want)
		}
	}
	wantDropin := "/etc/systemd/resolved.conf.d/bootwright-" + testControllerResolverOwnershipName + ".conf"
	if got := target["controllerResolverDropinPath"]; got != wantDropin {
		t.Fatalf("invalid record supplied cleanup path %v, want derived %s", got, wantDropin)
	}
}

func TestControllerResolverOwnershipNameMatchesAnsibleJSONIdentity(t *testing.T) {
	if got := controllerResolverOwnershipName("lab", v1alpha1.KindInfraComponent, "dns"); got != testControllerResolverOwnershipName {
		t.Fatalf("controller resolver ownership name = %q, want %q", got, testControllerResolverOwnershipName)
	}
}

func validControllerResolverRecord() ownership.ResourceRecord {
	return ownership.ResourceRecord{
		APIVersion: controllerResolverOwnershipAPIVersion,
		Kind:       string(ownership.KindControllerNameResolver),
		Name:       testControllerResolverOwnershipName,
		Owner:      ownership.Owner,
		Context:    "lab",
		Host:       "localhost",
		Provider:   v1alpha1.KindInfraComponent,
		Paths: []string{
			"/etc/systemd/resolved.conf.d/bootwright-" + testControllerResolverOwnershipName + ".conf",
		},
		Labels: map[string]string{
			"bootwright.kind": v1alpha1.ComponentSlotNameResolution,
			"bootwright.name": "dns",
		},
		Attributes: map[string]string{
			"resolver":    controllerResolverKind,
			"component":   "dns",
			"machineRef":  "host",
			"realisation": v1alpha1.InfraComponentTypeDnsmasq,
		},
	}
}

func controllerDestroyTargets(t *testing.T, vars map[string]any) []map[string]any {
	t.Helper()
	raw, ok := vars["bootwright_controller_name_resolution_destroy_targets"].([]any)
	if !ok {
		t.Fatalf("controller destroy targets = %#v", vars["bootwright_controller_name_resolution_destroy_targets"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		target, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("controller destroy target = %#v", item)
		}
		out = append(out, target)
	}
	return out
}
