package baremetal

import (
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Requests derives one frozen request per physical Machine, in canonical
// object order. It reads no host, endpoint or Secret material.
func Requests(catalog api.Catalog, controllerMachine, contextName string) ([]Request, error) {
	if !api.ValidLexical("name", contextName) {
		return nil, refusal("lifecycle.state", "the lifecycle context identity is invalid", "")
	}
	var requests []Request
	for _, provider := range substrate.ProvidersOn(catalog, substrate.ArmBaremetal) {
		for _, machine := range substrate.HostedMachines(catalog, provider.Name()) {
			request, err := requestFor(catalog, machine, controllerMachine, contextName)
			if err != nil {
				return nil, err
			}
			requests = append(requests, request)
		}
	}
	slices.SortFunc(requests, func(x, y Request) int { return strings.Compare(x.Identity.Object, y.Identity.Object) })
	return requests, nil
}

func requestFor(catalog api.Catalog, machine api.Object, controllerMachine, contextName string) (Request, error) {
	name := machine.Name()
	if !substrate.SafeSegment(name) {
		return Request{}, refusal("lifecycle.state", "the Machine name is not a safe host identifier", "rename "+machine.Identity())
	}
	target, err := substrate.TargetFor(catalog, machine, contextName, controllerMachine)
	if err != nil {
		return Request{}, err
	}
	placement, err := machineref.PlacementFor(target.PlacementMachine, controllerMachine)
	if err != nil {
		return Request{}, err
	}
	hardware := make([]Interface, 0, len(target.Interfaces))
	for _, declared := range target.Interfaces {
		hardware = append(hardware, Interface{MACAddress: declared.MACAddress, Name: declared.Name})
	}
	slices.SortFunc(hardware, func(x, y Interface) int { return strings.Compare(x.MACAddress, y.MACAddress) })
	return Request{
		Controller: Controller{
			CredentialsRef: target.Controller.CredentialsRef,
			Endpoint:       target.Controller.Endpoint,
			TLSVerify:      target.Controller.TLSVerify,
		},
		Hardware:  hardware,
		Identity:  Identity{Block: BlockID(name), Context: contextName, Object: name},
		Placement: placement,
		Provider:  target.Provider,
		Version:   requestVersion,
	}, nil
}

func refusal(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
