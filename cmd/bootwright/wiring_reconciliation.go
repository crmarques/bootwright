package main

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ansibleservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/custody"
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

// capabilityResolver is the immutable set of lifecycle capabilities this build
// offers. There is no registry, discovery or runtime plugin path.
type capabilityResolver struct{ artifactServer lifecycle.Capability }

func (r capabilityResolver) Resolve(kind, implementation string) (lifecycle.Capability, bool) {
	if kind != artifactserver.Kind || r.artifactServer == nil {
		return nil, false
	}
	if implementation != "" && implementation != artifactserver.Implementation {
		return nil, false
	}
	return r.artifactServer, true
}

func wireLifecycle(deps lifecycleDependencies, compiler compilation.Compiler, binder *custody.Service) cli.LifecycleService {
	if deps.Workspace == nil || deps.Inputs == nil || deps.Host == nil || deps.Guard == nil {
		return lifecycle.Service{}
	}
	clock := systemClock{}
	var capabilities lifecycle.CapabilityResolver = capabilityResolver{artifactServer: artifactserver.New(ansibleservice.New(), clock)}
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
// user's own marker, returning its identity so a stale marker is refused.
func currentSelection(store contexts.SelectionStore) lifecycle.CurrentSelection {
	if store == nil {
		return nil
	}
	return func(ctx context.Context) (string, string, error) {
		selected, err := store.Read(ctx)
		if err != nil {
			return "", "", err
		}
		return selected.Name, selected.ID, nil
	}
}
