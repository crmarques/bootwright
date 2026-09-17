package main

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation/ansiblerunner"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// lifecycleDependencies names every capability an operation consumes. A nil
// field leaves the lifecycle unavailable rather than substituting one.
type lifecycleDependencies struct {
	Workspace  lifecycle.Workspace
	Inputs     lifecycle.Inputs
	Host       lifecycle.HostIdentity
	Guard      lifecycle.ExecutionGuard
	Selection  contexts.SelectionStore
	Presenter  lifecycle.PlanPresenter
	Progress   lifecycle.ProgressReporter
	Confirmer  lifecycle.Confirmer
	Executable lifecycle.Executable
	// Capabilities substitutes the implementation set an operation may run.
	// Production leaves it empty and uses this build's own capabilities.
	Capabilities lifecycle.CapabilityResolver
}

// automationDigest is the content identity of the embedded collection this
// build would run. An operation freezes it; a continuation refuses on drift.
type automationDigest struct{}

func (automationDigest) CatalogDigest() string { return ansible.Digest() }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// capabilityResolver is the immutable, ordered set of lifecycle capabilities
// this build offers. There is no registry, discovery or runtime plugin path.
type capabilityResolver []boundCapability

type boundCapability struct {
	kind           string
	implementation string
	capability     lifecycle.Capability
}

func (r capabilityResolver) Bindings() []lifecycle.CapabilityBinding {
	bindings := make([]lifecycle.CapabilityBinding, 0, len(r))
	for _, bound := range r {
		bindings = append(bindings, lifecycle.CapabilityBinding{Kind: bound.kind, Implementation: bound.implementation})
	}
	return bindings
}

// Resolve answers the capability bound to exactly this kind and
// implementation, so a block frozen against one implementation never continues
// against another this build happens to offer for the same kind.
func (r capabilityResolver) Resolve(kind, implementation string) (lifecycle.Capability, bool) {
	for _, bound := range r {
		if bound.kind == kind && bound.implementation == implementation && bound.capability != nil {
			return bound.capability, true
		}
	}
	return nil, false
}

// buildCapabilities lists what this executable can realize, in the API's own
// kind order, so a plan's block order never depends on wiring order.
func buildCapabilities(clock systemClock, controller controllerDependencies) capabilityResolver {
	runner := ansiblerunner.New()
	resolver := capabilityResolver{{
		kind: clients.Kind, implementation: clients.Implementation,
		capability: clients.New(controller.Tools, controller.Native, controller.NativeInspector, controller.ClientInstaller),
	}, {
		kind: libvirt.MachineKind, implementation: libvirt.MachineImplementation,
		capability: libvirt.NewMachine(runner),
	}, {
		kind: baremetal.Kind, implementation: baremetal.Implementation,
		capability: baremetal.NewMachine(runner),
	}, {
		kind: installation.Kind, implementation: installation.Implementation,
		capability: installation.New(runner),
	}, {
		kind: libvirt.HostKind, implementation: libvirt.HostImplementation,
		capability: libvirt.NewHost(runner),
	}, {
		kind: agentinstall.Kind, implementation: agentinstall.MediaImplementation,
		capability: agentinstall.NewMedia(runner),
	}}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsserver.Definition(), ntpserver.Definition()} {
		resolver = append(resolver, boundCapability{
			kind: string(definition.Kind), implementation: definition.Implementation,
			capability: managedservice.NewCapability(definition, runner),
		})
	}
	return append(resolver, boundCapability{
		kind: artifactserver.Kind, implementation: artifactserver.Implementation,
		capability: artifactserver.New(runner, clock),
	})
}

func wireLifecycle(deps lifecycleDependencies, controller controllerDependencies, compiler compilation.Compiler, binder *custody.Service) lifecycle.Service {
	if deps.Workspace == nil || deps.Inputs == nil || deps.Host == nil || deps.Guard == nil {
		return lifecycle.Service{}
	}
	clock := systemClock{}
	var capabilities lifecycle.CapabilityResolver = buildCapabilities(clock, controller)
	if deps.Capabilities != nil {
		capabilities = deps.Capabilities
	}
	return lifecycle.New(deps.Workspace, deps.Inputs, compiler, binder, deps.Host, automationDigest{}, deps.Guard, capabilities, lifecycle.Options{
		Confirmer: deps.Confirmer, Presenter: deps.Presenter, Progress: deps.Progress,
		Clock: clock, Entropy: rand.Read, Selection: currentSelection(deps.Selection),
		Executable: deps.Executable,
		Operations: func(area operationstore.Area) lifecycle.OperationStore {
			return operationstore.New(area, clock.Now)
		},
	})
}

// currentSelection resolves an omitted context name through the invoking
// user's own marker. Every context-backed service binds this one reader, so a
// stale marker is caught in the same place for all of them.
func currentSelection(store contexts.SelectionStore) func(context.Context) (string, error) {
	if store == nil {
		return nil
	}
	return func(ctx context.Context) (string, error) {
		selected, err := store.Read(ctx)
		if err != nil {
			return "", err
		}
		return selected.Name, nil
	}
}
