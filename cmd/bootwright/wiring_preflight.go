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
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/preflight"
	"github.com/crmarques/bootwright/internal/storage/preflightlocal"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

func wireStoragePreflight(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) preflight.Service {
	return preflight.Service{Graph: effectiveGraph{state: state}, Ownership: machineOwnership{reconciler: reconciler}, Project: libvirt.CapacityAllocations, Capacity: capacity.Budget{Observer: preflightlocal.Observer{}}, Guests: preflight.GuestChecks{Commands: preflightCommands(state, reconciler, deps)}, Selection: currentSelection(deps.Selection)}
}

func libvirtCapacityAdmission(checker capacity.CapacityChecker) lifecycle.ApplyAdmission {
	if checker == nil {
		checker = capacity.Budget{Observer: preflightlocal.Observer{}}
	}
	return func(ctx context.Context, contextName string, catalog api.Catalog, owned map[string]lifecycle.ObjectOwnership, starting []reconciliation.Block) error {
		return libvirt.AdmitCapacity(ctx, contextName, catalog, owned, starting, checker)
	}
}

func preflightCommands(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) preflight.Commands {
	return preflight.CommandFunc(func(ctx context.Context, request preflight.CommandRequest) ([]byte, error) {
		capture := &preflightlocal.Capture{}
		client := sshlocal.New(deps.Home, deps.Owner, sessionFiles(deps.Files))
		sessions := machineaccess.New(state, currentSelection(deps.Selection), machineaccess.Options{
			Lender: reconciler, Ownership: machineOwnership{reconciler: reconciler}, Evidence: machineHostKeys{reconciler: reconciler}, Trust: deps.Trust, Launcher: client,
			Streams: machineaccess.Streams{Out: capture, Err: io.Discard},
		})
		result, err := sessions.Exec(ctx, machineaccess.ExecRequest{ContextName: request.ContextName, Names: []string{request.Machine}, SSH: request.SSH, Command: request.Argv})
		if preflight.UntrustedIdentity(err) {
			clear(capture.Data)
			if !request.ObserveUntrusted {
				return nil, preflight.UntrustedKey{}
			}
			key, observeErr := client.Observe(ctx, request.Address, request.Port)
			if observeErr != nil {
				return nil, preflight.UntrustedKey{}
			}
			return nil, preflight.UntrustedKey{Fingerprint: key.Fingerprint()}
		}
		if err != nil || result == nil || result.ExitCode != 0 || capture.Overflow {
			clear(capture.Data)
			return nil, errors.New("verified bounded prerequisite command did not complete")
		}
		return capture.Data, nil
	})
}

func wireContainerPreflight(state inventory.EffectiveState, reconciler lifecycle.Service, deps serviceDependencies) containerpreflight.Service {
	return containerpreflight.Service{Graph: effectiveGraph{state: state}, Selection: currentSelection(deps.Selection), Ownership: machineOwnership{reconciler: reconciler}, Project: libvirt.CapacityAllocations, Capacity: capacity.Budget{Observer: preflightlocal.Observer{}}, Resolver: containerpreflightlocal.Resolver{}, DNSBlock: dnsServerDefinition().BlockID, Blocks: func(ctx context.Context, name string) (map[string]string, error) {
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
