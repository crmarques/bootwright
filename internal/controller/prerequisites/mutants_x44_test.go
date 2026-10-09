package prerequisites

import (
	"encoding/json"
	"strings"
	"testing"
)

// A recovered native transaction records the evidence its recovery returned on
// the container-runtime action, not the bare postcondition an unchanged action
// carries, so the receipt says what the recovery proved (X33's mutants, B255).
func TestARecoveredNativeTransactionRecordsItsOwnEvidence(t *testing.T) {
	f, _ := explicitRuntimeFixture(t)
	if report, err := recoveredSetup(t, f); err != nil || report.Outcome != "changed" {
		t.Fatalf("recovered setup = %#v (%v)", report, err)
	}
	for _, action := range f.store.state.Receipt.Actions {
		if action.ID != "container-runtime" {
			continue
		}
		var evidence map[string]any
		if err := json.Unmarshal(action.Evidence, &evidence); err != nil || evidence["nativePostcondition"] != "verified" || action.Outcome != "changed" {
			t.Fatalf("the recovered action records %s, outcome %s (%v)", action.Evidence, action.Outcome, err)
		}
		return
	}
	t.Fatal("the receipt holds no container-runtime action")
}

// The podman root is proved against podman's own request, never another
// requirement's: a plan whose podman root matches only the libvirt request is
// refused (X33's mutants, B255).
func TestThePodmanRootIsProvedAgainstItsOwnRequest(t *testing.T) {
	plan := nativePlanFixture()
	plan.Requirements.ContainerRuntime = true
	plan.Requests.Podman, plan.Requests.Libvirt = "5.6.1", "11.5.0"
	identity := NativeIdentity{Name: "podman", Version: "5.6.1", Release: "1.fc43", Architecture: "x86_64"}
	plan.Roots = append(plan.Roots, NativeRoot{Key: "podman", Requested: "5.6.1", Package: identity})
	plan.Packages = append(plan.Packages, NativePackage{
		Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture,
		Signer: strings.Repeat("c", 40), Source: DependencySource{ID: "podman", URL: "https://publisher.example.test/os/podman.rpm", SHA256: strings.Repeat("d", 64), Bytes: 64},
	})
	canonical, err := CanonicalNativePlan(plan)
	if err != nil || ValidateNativePlan(canonical) != nil {
		t.Fatalf("the podman root under its own request was refused: %v", err)
	}
	swapped := canonical
	swapped.Requests.Podman, swapped.Requests.Libvirt = "11.5.0", "5.6.1"
	if ValidateNativePlan(swapped) == nil {
		t.Fatal("a podman root proved against another request was accepted")
	}
}
