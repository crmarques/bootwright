package diskdiscovery

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
		if object.Kind() == api.Machine && object.Spec().Has("hardware", "bmc") {
			objects[index] = object.WithSpec(object.Spec().WithPath(api.BoolValue(true), "hardware", "bmc", "virtualMedia", "tls", "enableVerification"))
		}
	}
	catalog = api.NewCatalog(objects)
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(nil).WithMedia(labMedia()).Plan(context.Background(), input)
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
