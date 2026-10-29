package power

import (
	"slices"
	"strings"

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
	if err := undeclared(catalog, contextName, []string{name}); err != nil {
		return Request{}, false, err
	}
	object, _ := catalog.Find(api.Machine, name)
	controllerMachine, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return Request{}, false, err
	}
	arm, controller, placement, physical, err := controllerFor(catalog, object, contextName, controllerMachine, realized)
	if err != nil {
		return Request{}, false, err
	}
	return Request{
		Arm: arm, Controller: controller, Force: force,
		Identity:  Identity{Context: contextName, Object: object.Name()},
		Placement: placement, Verb: verb, Version: Implementation,
	}, physical, nil
}

// undeclared refuses every name the context declares no Machine for at once.
func undeclared(catalog api.Catalog, contextName string, names []string) error {
	var missing []string
	for _, name := range names {
		if _, ok := catalog.Find(api.Machine, name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	message := "the selected context declares no Machine named " + missing[0]
	if len(missing) > 1 {
		message = "the selected context declares no Machines named " + strings.Join(missing, ", ")
	}
	return failure("access.target", message, "list the Machines this context selects with bootwright machine list --context "+contextName)
}

// controllerFor resolves the endpoint this Machine is managed through, the
// host that reaches it, and whether the Machine is physical. A Machine the
// substrate realizes is reached through the controller that realization
// created, so it answers only while that realization stands; a physical
// Machine's controller exists without Bootwright and is never asked about.
func controllerFor(catalog api.Catalog, object api.Object, contextName, controllerMachine string, realized realizations) (string, Access, machine.Placement, bool, error) {
	target, err := managedTarget(catalog, object, contextName, controllerMachine)
	if err != nil {
		return "", Access{}, machine.Placement{}, false, err
	}
	if target.Controller.HostCluster != "" {
		return "", Access{}, machine.Placement{}, false, failure("access.unavailable",
			"power operations do not yet reach a KubeVirt Machine whose provider is hosted by OpenShiftCluster/"+target.Controller.HostCluster,
			"stop its VirtualMachine through that cluster's API, then repeat the command that needed it stopped")
	}
	if !target.Physical {
		admitted, err := reachable(realized, object.Name())
		if err != nil {
			return "", Access{}, machine.Placement{}, false, err
		}
		if !admitted {
			return "", Access{}, machine.Placement{}, false, failure("access.unavailable",
				"this Machine's emulated management controller is not realized",
				"bootwright apply --context "+contextName+" realizes "+object.Identity()+" and its emulated BMC")
		}
	}
	access, placement, physical, err := endpointFor(target, controllerMachine)
	return target.Port, access, placement, physical, err
}

// managedTarget resolves the realized target whose controller manages a
// Machine, without asking whether that controller exists yet.
func managedTarget(catalog api.Catalog, object api.Object, contextName, controllerMachine string) (substrate.Target, error) {
	if object.Spec().Get("substrate", "providerRef").Text() == "" && !substrate.MachineBMC(object).Present() {
		return substrate.Target{}, failure("access.unavailable",
			"this Machine has no management controller this context can reach",
			"declare "+substrate.BMCPath+" on "+object.Identity()+", or place it on a provider that emulates a management controller")
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

func endpointFor(target substrate.Target, controllerMachine string) (Access, machine.Placement, bool, error) {
	placement, err := machine.PlacementFor(target.PlacementMachine, controllerMachine)
	if err != nil {
		return Access{}, machine.Placement{}, false, err
	}
	return Access{
		CredentialsRef: target.Controller.CredentialsRef,
		Endpoint:       target.Controller.Endpoint,
		TLSVerify:      target.Controller.TLSVerify,
		TrustBundleRef: target.Controller.TrustBundleRef,
	}, placement, target.Physical, nil
}
