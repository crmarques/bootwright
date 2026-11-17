package installation

import (
	"context"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestDiscoveryPlanFreezesVerificationSetupWithoutRunningIt(t *testing.T) {
	catalog := discoveryCatalog()
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Spec().Has("substrate", "baremetal", "bmc") {
			objects[index] = object.WithSpec(object.Spec().WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "enableVerification"))
		}
	}
	catalog = api.NewCatalog(objects)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := NewDiscovery(nil).WithMedia(labMedia()).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("plan: %v %v", plan, err)
	}
	block := plan.Definitions[0]
	request, err := DecodeDiscoveryRequest(block.Request)
	if err != nil || !request.Controller.VirtualMedia.EnableVerification {
		t.Fatalf("frozen verification policy: %+v %v", request.Controller, err)
	}
	key := infrastructureservices.ControllerMediaReservationKey(request.Controller.Endpoint).String()
	if !slices.Contains(block.Exclusive, key) || !slices.ContainsFunc(plan.Reservations, func(claim infrastructureservices.HostReservation) bool { return slices.Contains(claim.Keys, key) }) {
		t.Fatalf("shared-manager reservation absent: %v %v", block.Exclusive, plan.Reservations)
	}
	if !slices.ContainsFunc(block.Impacts, func(impact string) bool { return strings.HasPrefix(impact, "enable-bmc-media-verification ") }) {
		t.Fatalf("setup effect absent from preview: %v", block.Impacts)
	}
}

func TestPhysicalMediaReservationsCoverOtherSystemsAndContexts(t *testing.T) {
	first := Request{Target: Target{Physical: true, Controller: Controller{Endpoint: "https://bmc.example.test/redfish/v1/Systems/1"}}}
	second := first
	second.Target.Controller.Endpoint = "https://bmc.example.test:443/redfish/v1/Systems/2"
	if !slices.Equal(first.ExclusiveKeys(), second.ExclusiveKeys()) {
		t.Fatalf("shared manager is not serialized: %v %v", first.ExclusiveKeys(), second.ExclusiveKeys())
	}
	held := []infrastructureservices.HostReservation{{Context: "first", Kind: "os-install", Service: "node-a", Keys: first.ReservationKeys()}}
	wanted := []infrastructureservices.HostReservation{{Context: "second", Kind: "os-install", Service: "node-b", Keys: second.ReservationKeys()}}
	if len(infrastructureservices.Conflicts(held, wanted)) != 1 {
		t.Fatal("different contexts could concurrently mutate one manager")
	}
	if got := (Request{}).ExclusiveKeys(); len(got) != 0 {
		t.Fatalf("virtual requests acquired a physical BMC claim: %v", got)
	}
}

func TestInstallationAndEraseKeepTheExplicitVerificationPolicy(t *testing.T) {
	m := metalOn("eno1", hardwareNIC("eno1", "52:54:00:9a:1b:01"))
	m = m.WithSpec(m.Spec().WithPath(api.BoolValue(true), "substrate", "baremetal", "bmc", "virtualMedia", "tls", "enableVerification"))
	request := requestOf(t, metalCatalog(m, metalNetwork(declaredInterface("eno1", "ethernet"))), m.Name())
	if !request.Target.Controller.VirtualMedia.EnableVerification {
		t.Fatal("installation lost verification setup intent")
	}
	erase := EraseRequest{Install: request}
	if !slices.ContainsFunc(eraseImpacts(erase), func(impact string) bool { return strings.HasPrefix(impact, "enable-bmc-media-verification ") }) {
		t.Fatal("erase preview lost verification setup intent")
	}
}
