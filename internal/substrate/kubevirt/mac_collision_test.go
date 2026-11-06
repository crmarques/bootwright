package kubevirt

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

func collidingNames(t *testing.T) (string, string) {
	t.Helper()
	seen := map[string]string{}
	for index := range 1 << 16 {
		name := fmt.Sprintf("node-%d", index)
		mac := substrate.InterfaceMAC(testContext, name, "enp1s0")
		if held, found := seen[mac]; found {
			return held, name
		}
		seen[mac] = name
	}
	t.Fatal("no two names derive one MAC address")
	return "", ""
}

func TestTwoMachinesDerivingOneMACRefuseBeforeRegistration(t *testing.T) {
	first, second := collidingNames(t)
	mac := substrate.InterfaceMAC(testContext, first, "enp1s0")
	catalog := labCatalog(provider(), node(first), node(second))
	_, err := HostRequests(catalog, "controller", testContext)
	if err == nil {
		t.Fatal("two Machines deriving one MAC were planned")
	}
	issues := diagnostics.Of(err)
	if len(issues) != 1 {
		t.Fatalf("issues %+v", issues)
	}
	for _, want := range []string{"Machine/" + first + " interface enp1s0", "Machine/" + second + " interface enp1s0", mac} {
		if !strings.Contains(issues[0].Message, want) {
			t.Errorf("the refusal does not name %q: %+v", want, issues[0])
		}
	}
	if !strings.Contains(issues[0].Remediation, "rename") {
		t.Errorf("no remedy: %+v", issues[0])
	}
	plan, err := NewHost(nil, nil).Plan(t.Context(), planInput(catalog, "apply"))
	if err == nil || len(plan.Definitions) != 0 {
		t.Fatalf("the provider proof planned: %+v, %v", plan, err)
	}
	if requests, err := HostRequests(labCatalog(provider(), node(first)), "controller", testContext); err != nil || len(requests) != 1 {
		t.Fatalf("one Machine: %v, %v", requests, err)
	}
}
