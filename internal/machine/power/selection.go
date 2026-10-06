package power

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// realizations answers the Realization port for the Machines of one context.
// A nil value answers that this context realizes nothing.
type realizations func(name string) (machine.OwnershipState, bool, error)

// requestFor derives the frozen power request for one exact Machine. Power
// always goes through the Machine's own management controller, so a virtual
// and a physical server take the same path: the realized target answers where
// that controller is and how its transport is trusted, and this package never
// learns which substrate answered. Whether the Machine is physical travels
// beside the request rather than in it: it decides whether the operation holds
// the Machine to a proved identity first, never what the adapter is asked.
func requestFor(catalog api.Catalog, contextName, name, verb string, force bool, realized realizations) (Request, bool, error) {
	if !slices.Contains([]string{Start, Stop, Restart}, verb) {
		return Request{}, false, failure("lifecycle.state", "unsupported power verb", "")
	}
	object, ok := catalog.Find(api.Machine, name)
	if !ok {
		return Request{}, false, failure("access.target", "the selected context declares no Machine named "+name,
			"list the Machines this context selects with bootwright machine list --context "+contextName)
	}
	controllerMachine, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return Request{}, false, err
	}
	controller, placement, physical, err := controllerFor(catalog, object, contextName, controllerMachine, realized)
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
// created, so it answers only while that realization stands; a physical
// Machine's controller exists without Bootwright and is never asked about.
func controllerFor(catalog api.Catalog, object api.Object, contextName, controllerMachine string, realized realizations) (Controller, machineref.Placement, bool, error) {
	target, err := managedTarget(catalog, object, contextName, controllerMachine)
	if err != nil {
		return Controller{}, machineref.Placement{}, false, err
	}
	if !target.Physical {
		admitted, err := reachable(realized, object.Name())
		if err != nil {
			return Controller{}, machineref.Placement{}, false, err
		}
		if !admitted {
			return Controller{}, machineref.Placement{}, false, failure("access.unavailable",
				"this Machine's emulated management controller is not realized",
				"bootwright apply --context "+contextName+" realizes "+object.Identity()+" and its controller")
		}
	}
	return endpointFor(target, controllerMachine)
}

// managedTarget resolves the realized target whose controller manages a
// Machine, without asking whether that controller exists yet.
func managedTarget(catalog api.Catalog, object api.Object, contextName, controllerMachine string) (substrate.Target, error) {
	if object.Spec().Get("substrate", "providerRef").Text() == "" && !object.Spec().Has("hardware", "management", "bmc") {
		return substrate.Target{}, failure("access.unavailable",
			"this Machine has no management controller this context can reach",
			"author spec.hardware.management.bmc on "+object.Identity()+", or place it on a provider that emulates one")
	}
	return substrate.TargetFor(catalog, object, contextName, controllerMachine)
}

// reachable reports whether the block that realizes a Machine on its provider
// stands, whatever the Machine's installation reached. An apply that completed
// it created the controller, and a removal that has not proved it gone has not
// yet taken the controller back, so a guest a removal refuses can be stopped.
func reachable(realized realizations, name string) (bool, error) {
	if realized == nil {
		return false, nil
	}
	state, found, err := realized(name)
	if err != nil || !found {
		return false, err
	}
	switch state.Verb {
	case machine.VerbApply:
		return state.State == machine.BlockDone, nil
	case machine.VerbDestroy:
		return state.State != machine.BlockDone, nil
	}
	return false, nil
}

func endpointFor(target substrate.Target, controllerMachine string) (Controller, machineref.Placement, bool, error) {
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
