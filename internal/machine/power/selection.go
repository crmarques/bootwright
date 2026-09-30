package power

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// requestFor derives the frozen power request for one exact Machine. Power
// always goes through the Machine's own management controller, so a virtual
// and a physical server take the same path: the realized target answers where
// that controller is and how its transport is trusted, and this package never
// learns which substrate answered. Whether the Machine is physical travels
// beside the request rather than in it: it decides whether the operation holds
// the Machine to a proved identity first, never what the adapter is asked.
func requestFor(catalog api.Catalog, contextName, name, verb string, force bool, owned map[string]machine.OwnershipState) (Request, bool, error) {
	if !slices.Contains([]string{Start, Stop, Restart}, verb) {
		return Request{}, false, failure("lifecycle.state", "unsupported power verb", "")
	}
	object, ok := catalog.Find(api.Machine, name)
	if !ok {
		return Request{}, false, failure("access.target", "the selected context declares no Machine named "+name,
			"list the Machines this context selects with bootwright machine list")
	}
	controllerMachine, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return Request{}, false, err
	}
	controller, placement, physical, err := controllerFor(catalog, object, contextName, controllerMachine, owned)
	if err != nil {
		return Request{}, false, err
	}
	return Request{
		Controller: controller, Force: force,
		Identity:  Identity{Context: contextName, Object: object.Name()},
		Placement: placement, Verb: verb, Version: Implementation,
	}, physical, nil
}

// controllerFor resolves the endpoint this Machine is managed through, the
// host that reaches it, and whether the Machine is physical. A Machine the
// substrate realizes is reached through the controller that realization
// created, so it answers only while this context owns it; a physical Machine's
// controller exists without Bootwright and needs no ownership at all.
func controllerFor(catalog api.Catalog, object api.Object, contextName, controllerMachine string, owned map[string]machine.OwnershipState) (Controller, machineref.Placement, bool, error) {
	if object.Spec().Get("substrate", "providerRef").Text() == "" && !object.Spec().Has("hardware", "management", "bmc") {
		return Controller{}, machineref.Placement{}, false, failure("access.unavailable",
			"this Machine has no management controller this context can reach",
			"author spec.hardware.management.bmc on "+object.Identity()+", or place it on a provider that emulates one")
	}
	target, err := substrate.TargetFor(catalog, object, contextName, controllerMachine)
	if err != nil {
		return Controller{}, machineref.Placement{}, false, err
	}
	if !target.Physical && !owned[object.Identity()].Realized() {
		return Controller{}, machineref.Placement{}, false, failure("access.unavailable",
			"this Machine's emulated management controller is not realized",
			"apply this context so "+object.Identity()+" and its controller exist")
	}
	placement, err := machineref.PlacementFor(target.PlacementMachine, controllerMachine)
	if err != nil {
		return Controller{}, machineref.Placement{}, false, err
	}
	return Controller{
		CredentialsRef: target.Controller.CredentialsRef,
		Endpoint:       target.Controller.Endpoint,
		TLSVerify:      target.Controller.TLSVerify,
		TrustBundleRef: target.Controller.TrustBundleRef,
	}, placement, target.Physical, nil
}
