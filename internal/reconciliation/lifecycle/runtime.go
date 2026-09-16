package lifecycle

import (
	"context"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// RuntimeRequest names the context whose approved bundle a bounded operation
// runs inside, and the Secret declarations that operation needs bound.
type RuntimeRequest struct {
	ContextName string
	Secrets     []string
}

// Runtime is the private execution boundary one bounded adapter call runs in.
// Material is bounded memory owned by the lender and cleared when the call
// returns, so nothing it carries outlives the operation that asked for it.
type Runtime struct {
	Context  ContextIdentity
	Launch   prerequisites.PythonLaunch
	Bundle   prerequisites.BundleLocation
	Area     prerequisites.BundleArea
	Material map[string]secrets.Material
}

// WithRuntime lends the controller's approved execution boundary to one
// bounded operation outside the lifecycle. It registers no operation, freezes
// no plan and publishes no evidence: an operation that proves nothing about
// desired state must not be able to claim, release or continue ownership.
// The context is read under its shared lock for the whole call, so a lifecycle
// mutation cannot run against the same host at the same time.
func (s Service) WithRuntime(ctx context.Context, request RuntimeRequest, call func(context.Context, Runtime) error) error {
	if err := s.available(ctx); err != nil {
		return err
	}
	if call == nil {
		return failure("lifecycle.state", "a bounded runtime request carries no operation", "")
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return err
	}
	// Binding happens outside the store lock, exactly as a registered
	// operation binds before it opens its transaction.
	binding, material, err := s.lend(ctx, name, request.Secrets)
	if err != nil {
		return err
	}
	defer func() {
		clearMaterial(material)
		if binding != "" {
			_, _ = s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: binding})
		}
	}()
	return s.workspace.ReadLifecycle(ctx, name, func(view View) error {
		area, location, requirement, err := approvedBundle(ctx, view)
		if err != nil {
			return err
		}
		return s.guard.WithPython(ctx, area, requirement, func(launch prerequisites.PythonLaunch, _ func() error) error {
			return call(ctx, Runtime{
				Context: view.Identity(), Launch: launch, Bundle: location, Area: area, Material: material,
			})
		})
	})
}

// lend freezes exactly the Secret versions one bounded operation reads. The
// binding is transient: it exists so the operation reads a coherent set, and
// is released as soon as the call returns.
func (s Service) lend(ctx context.Context, name string, references []string) (string, map[string]secrets.Material, error) {
	if len(references) == 0 {
		return "", map[string]secrets.Material{}, nil
	}
	result, err := s.binder.Bind(ctx, custody.BindRequest{ContextName: name, Names: references})
	if err != nil {
		return "", nil, err
	}
	bound, err := s.binder.Reopen(ctx, custody.BindingRequest{ContextName: name, BindingID: result.ID})
	if err != nil {
		_, _ = s.binder.Release(ctx, custody.BindingRequest{ContextName: name, BindingID: result.ID})
		return "", nil, err
	}
	material := make(map[string]secrets.Material, len(bound))
	for _, item := range bound {
		material[item.Version.Declaration.Name] = item.Material
	}
	return result.ID, material, nil
}

// approvedBundle opens the execution bundle the retained controller setup
// approved for this context, with the requirement its runtime must satisfy.
func approvedBundle(ctx context.Context, view View) (prerequisites.BundleArea, prerequisites.BundleLocation, prerequisites.ExecutionRequirement, error) {
	var requirement prerequisites.ExecutionRequirement
	receipt := view.Controller().State.Receipt
	if receipt.Definition == nil {
		return nil, prerequisites.BundleLocation{}, requirement, failure("controller.state",
			"the retained controller setup has no execution definition", "run bootwright setup")
	}
	if view.Controller().OpenBundle == nil {
		return nil, prerequisites.BundleLocation{}, requirement, failure("controller.state", "the approved execution bundle is unavailable", "run bootwright setup")
	}
	area, err := view.Controller().OpenBundle(ctx, receipt.CatalogDigest)
	if err != nil {
		return nil, prerequisites.BundleLocation{}, requirement, err
	}
	if area == nil {
		return nil, prerequisites.BundleLocation{}, requirement, failure("controller.state", "the approved execution bundle is missing", "run bootwright setup")
	}
	location, err := area.Location(ctx)
	if err != nil {
		return nil, prerequisites.BundleLocation{}, requirement, err
	}
	return area, location, receipt.Definition.Execution, nil
}
