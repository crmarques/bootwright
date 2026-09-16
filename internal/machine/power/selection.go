package power

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// requestFor derives the frozen power request for one exact Machine. Power
// always goes through the Machine's own management controller, so a virtual
// and a physical server take the same path: only where the endpoint comes from
// differs, and an emulated controller exists only once its Machine is realized.
func requestFor(catalog api.Catalog, contextName, name, verb string, force bool, owned map[string]machine.OwnershipState) (Request, error) {
	if !slices.Contains([]string{Start, Stop, Restart}, verb) {
		return Request{}, failure("lifecycle.state", "unsupported power verb", "")
	}
	object, ok := catalog.Find(api.Machine, name)
	if !ok {
		return Request{}, failure("access.target", "the selected context declares no Machine named "+name,
			"list the Machines this context selects with bootwright machine list")
	}
	controllerMachine, err := lifecycle.ControllerMachine(catalog)
	if err != nil {
		return Request{}, err
	}
	controller, placement, err := controllerFor(catalog, object, contextName, controllerMachine, owned)
	if err != nil {
		return Request{}, err
	}
	return Request{
		Controller: controller, Force: force,
		Identity:  Identity{Context: contextName, Object: object.Name()},
		Placement: placement, Verb: verb, Version: Implementation,
	}, nil
}

// controllerFor resolves the endpoint this Machine is managed through and the
// host that reaches it. An authored controller belongs to the machine itself
// and answers whatever this context has realized; an emulated one is a Machine
// effect, so it answers only while that Machine is owned.
func controllerFor(catalog api.Catalog, object api.Object, contextName, controllerMachine string, owned map[string]machine.OwnershipState) (Controller, lifecycle.Placement, error) {
	if bmc := object.Spec().Get("hardware", "management", "bmc"); bmc.Present() {
		return authoredController(catalog, object, bmc, controllerMachine)
	}
	return emulatedController(catalog, object, contextName, controllerMachine, owned)
}

func authoredController(catalog api.Catalog, object api.Object, bmc api.Value, controllerMachine string) (Controller, lifecycle.Placement, error) {
	endpoint := bmc.Get("address").Text()
	if endpoint == "" {
		return Controller{}, lifecycle.Placement{}, failure("api.required", "the Machine's management controller declares no address",
			"set spec.hardware.management.bmc.address on "+object.Identity())
	}
	credentials := bmc.Get("credentialsRef").Text()
	if credentials == "" {
		return Controller{}, lifecycle.Placement{}, failure("api.required", "the Machine's management controller declares no credential",
			"set spec.hardware.management.bmc.credentialsRef on "+object.Identity())
	}
	placement, err := controllerPlacement(catalog, controllerMachine)
	return Controller{CredentialsRef: credentials, Endpoint: endpoint}, placement, err
}

func emulatedController(catalog api.Catalog, object api.Object, contextName, controllerMachine string, owned map[string]machine.OwnershipState) (Controller, lifecycle.Placement, error) {
	reference := object.Spec().Get("substrate", "providerRef").Text()
	provider, ok := catalog.Find(api.InfraProvider, reference)
	if !ok {
		return Controller{}, lifecycle.Placement{}, failure("access.unavailable", "this Machine has no management controller this context can reach",
			"author spec.hardware.management.bmc on "+object.Identity()+", or place it on a provider that emulates one")
	}
	if !provider.Spec().Has("libvirt") {
		return Controller{}, lifecycle.Placement{}, failure("access.unavailable", "power operations support only an authored or libvirt-emulated management controller",
			"author spec.hardware.management.bmc on "+object.Identity())
	}
	if !owned[object.Identity()].Realized() {
		return Controller{}, lifecycle.Placement{}, failure("access.unavailable", "this Machine's emulated management controller is not realized",
			"apply this context so "+object.Identity()+" and its controller exist")
	}
	port, ok := substrate.ControllerPort(catalog, provider, object.Name())
	if !ok {
		return Controller{}, lifecycle.Placement{}, failure("api.value", "the Machine's emulated controller port does not allocate",
			"correct spec.libvirt.bmcEmulationDefaults.port on "+provider.Identity())
	}
	credentials := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "auth", "credentialsRef").Text()
	if credentials == "" {
		return Controller{}, lifecycle.Placement{}, failure("api.required", "the provider declares no emulated controller credential",
			"set spec.libvirt.bmcEmulationDefaults.auth.credentialsRef on "+provider.Identity())
	}
	host, ok := catalog.Find(api.Machine, provider.Spec().Get("libvirt", "machineRef").Text())
	if !ok {
		return Controller{}, lifecycle.Placement{}, failure("api.reference", "the provider's host Machine is not in the selected graph",
			"declare it or correct spec.libvirt.machineRef on "+provider.Identity())
	}
	placement, err := lifecycle.PlacementFor(host, controllerMachine)
	if err != nil {
		return Controller{}, lifecycle.Placement{}, err
	}
	address := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text()
	endpoint := substrate.ControllerEndpoint(address, port, substrate.DomainUUID(contextName, object.Name()))
	return Controller{CredentialsRef: credentials, Endpoint: endpoint}, placement, nil
}

// controllerPlacement runs the operation on the controller itself, which is
// where an authored management endpoint is reached from.
func controllerPlacement(catalog api.Catalog, controllerMachine string) (lifecycle.Placement, error) {
	object, ok := catalog.Find(api.Machine, controllerMachine)
	if !ok {
		return lifecycle.Placement{}, failure("api.reference", "the Environment's controller Machine is not in the selected graph",
			"declare it or correct spec.controller.machineRef")
	}
	return lifecycle.PlacementFor(object, controllerMachine)
}
