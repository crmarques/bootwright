package main

import (
	"context"
	"errors"
	"io"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	containerpreflightlocal "github.com/crmarques/bootwright/internal/containercluster/preflightlocal"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/sshlocal"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/storage/preflightlocal"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/trust"
)

func wireStoragePreflight(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) preflight.Service {
	return preflight.Service{Graph: effectiveGraph{state: state}, Ownership: machineOwnership{reconciler: reconciler}, Project: libvirt.CapacityAllocations, Writes: preflightWrites(deps), Capacity: capacity.Budget{Observer: remoteObserver(state, &reconciler, deps)}, Guests: preflight.GuestChecks{Commands: preflightCommands(state, reconciler, deps)}, HostPorts: preflightHostPorts, Selection: currentSelection(deps.Selection)}
}

func libvirtCapacityAdmission(checker capacity.CapacityChecker) lifecycle.ApplyAdmission {
	if checker == nil {
		checker = capacity.Budget{Observer: preflightlocal.Observer{}}
	}
	return func(ctx context.Context, contextName string, catalog api.Catalog, owned map[string]lifecycle.ObjectOwnership, starting []reconciliation.Block) error {
		return libvirt.AdmitCapacity(ctx, contextName, catalog, owned, starting, checker, installation.CapacityWrites(installation.FrozenSizes(starting)))
	}
}

func preflightWrites(deps serviceDependencies) capacity.WriteProjector {
	return installation.CapacityWrites(installation.StoreSizes(lifecycleMedia(deps.Lifecycle)))
}

func remoteObserver(state inventory.EffectiveState, reconciler *lifecycle.Service, deps serviceDependencies) preflightlocal.Observer {
	return preflightlocal.Observer{Remote: remoteCommands{state: state, reconciler: reconciler, deps: deps}}
}

func remoteCapacity(state inventory.EffectiveState, reconciler *lifecycle.Service, deps serviceDependencies) capacity.CapacityChecker {
	return capacity.Budget{Observer: remoteObserver(state, reconciler, deps)}
}

type remoteCommands struct {
	state      inventory.EffectiveState
	reconciler *lifecycle.Service
	deps       serviceDependencies
}

func (r remoteCommands) Run(ctx context.Context, contextName, machine string, argv []string, bound int) ([]byte, int, error) {
	if r.reconciler == nil {
		return nil, 0, errors.New("verified remote command is unavailable")
	}
	capture := &preflightlocal.Capture{Limit: bound}
	client := sshlocal.New(r.deps.Home, r.deps.Owner, sessionFiles(r.deps.Files))
	sessions := machineaccess.New(r.state, currentSelection(r.deps.Selection), machineaccess.Options{
		Lender: *r.reconciler, Ownership: machineOwnership{reconciler: *r.reconciler}, Evidence: machineHostKeys{reconciler: *r.reconciler}, Trust: r.deps.Trust, Launcher: client,
		Streams: machineaccess.Streams{Out: capture, Err: io.Discard},
	})
	result, err := sessions.Exec(ctx, machineaccess.ExecRequest{ContextName: contextName, Names: []string{machine}, Command: argv})
	if err != nil || result == nil || capture.Overflow {
		clear(capture.Data)
		return nil, 0, errors.New("verified bounded remote command did not complete")
	}
	if result.ExitCode != 0 {
		clear(capture.Data)
		return nil, result.ExitCode, nil
	}
	return capture.Data, 0, nil
}

func preflightCommands(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) preflight.Commands {
	base := machineaccess.Options{Lender: reconciler, Ownership: machineOwnership{reconciler: reconciler}, Evidence: machineHostKeys{reconciler: reconciler}, Trust: deps.Trust}
	return preflightReads(state, currentSelection(deps.Selection), base, func() preflightClient {
		return sshlocal.New(deps.Home, deps.Owner, sessionFiles(deps.Files))
	})
}

type preflightClient interface {
	machineaccess.Launcher
	Observe(ctx context.Context, address string, port int) (trust.HostKey, error)
}

func preflightReads(state machineaccess.EffectiveState, selection machineaccess.CurrentSelection, base machineaccess.Options, client func() preflightClient) preflight.Commands {
	return preflight.CommandFunc(func(ctx context.Context, request preflight.CommandRequest) ([]byte, error) {
		capture := &preflightlocal.Capture{}
		launcher := client()
		options := base
		options.Launcher, options.Streams, options.FleetIdentity = launcher, machineaccess.Streams{Out: capture, Err: io.Discard}, requestFleetIdentity(request)
		sessions := machineaccess.New(state, selection, options)
		result, err := sessions.Exec(ctx, machineaccess.ExecRequest{ContextName: request.ContextName, Names: []string{request.Machine}, SSH: request.SSH, Command: request.Argv})
		if preflight.UntrustedIdentity(err) {
			clear(capture.Data)
			if !request.ObserveUntrusted {
				return nil, preflight.UntrustedKey{}
			}
			key, observeErr := launcher.Observe(ctx, request.Address, request.Port)
			if observeErr != nil {
				return nil, preflight.UntrustedKey{}
			}
			return nil, preflight.UntrustedKey{Fingerprint: key.Fingerprint()}
		}
		if err == nil && result != nil && result.ExitCode == sshTransportExit {
			clear(capture.Data)
			return nil, preflight.ErrUnreached
		}
		if err == nil && result != nil && result.ExitCode != 0 && !capture.Overflow {
			clear(capture.Data)
			return nil, preflight.ExitStatus{Code: result.ExitCode}
		}
		if err != nil || result == nil || result.ExitCode != 0 || capture.Overflow {
			clear(capture.Data)
			return nil, errors.New("verified bounded prerequisite command did not complete")
		}
		return capture.Data, nil
	})
}

const sshTransportExit = 255

func requestFleetIdentity(request preflight.CommandRequest) func(api.Catalog, api.Object) (machineaccess.FleetIdentity, bool) {
	return func(_ api.Catalog, object api.Object) (machineaccess.FleetIdentity, bool) {
		if request.Fleet == nil || request.Fleet.KeyRef == "" || object.Name() != request.Machine {
			return machineaccess.FleetIdentity{}, false
		}
		return machineaccess.FleetIdentity{User: request.Fleet.User, KeyRef: request.Fleet.KeyRef}, true
	}
}

func wireContainerPreflight(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) containerpreflight.Service {
	return containerpreflight.Service{Graph: effectiveGraph{state: state}, Selection: currentSelection(deps.Selection), Ownership: machineOwnership{reconciler: reconciler}, Project: libvirt.CapacityAllocations, Writes: preflightWrites(deps), Capacity: capacity.Budget{Observer: remoteObserver(state, &reconciler, deps)}, Resolver: containerpreflightlocal.Resolver{}, DNSBlock: dnsServerDefinition().BlockID, Blocks: func(ctx context.Context, name string) (map[string]string, error) {
		result, err := reconciler.Status(ctx, lifecycle.StatusRequest{ContextName: name})
		if err != nil {
			return nil, err
		}
		states := map[string]string{}
		if result.Lifecycle != nil && result.Lifecycle.Verb == "apply" {
			for _, block := range result.Lifecycle.Blocks {
				states[block.ID] = block.State
			}
		}
		return states, nil
	}}
}
