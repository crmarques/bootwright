package lifecycle

import (
	"context"
	"path"
	"slices"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// runOutputName is what one bounded run retains beside nothing else: it
// publishes no structured events, so there is no log for its output to sit
// next to.
const runOutputName = "run.output"

// RuntimeRequest names the context whose approved bundle a bounded operation
// runs inside, and the Secret declarations that operation needs bound.
// RetainOutput is set by a caller that names its run's output to an operator;
// one that names none, such as a reading, leaves it unset, and its run keeps
// nothing and discards what its adapter prints.
type RuntimeRequest struct {
	ContextName  string
	Secrets      []string
	RetainOutput bool
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
	// Output retains what this run's adapter prints. LogLocation names the
	// directory holding it on this host, and Logs names the same file relative
	// to the state root. A caller hands Output to the adapter and names both in
	// its result; nothing reads them back, and a retention fault never changes
	// what the run reports. OutputRemediation is what an adapter failure its
	// output explains tells an operator to read, for a caller that names the
	// file; one that names none leaves it out of its request. All four are
	// empty for a run whose request retains nothing.
	Output            prerequisites.RunOutput
	LogLocation       string
	Logs              []string
	OutputRemediation string
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
	return s.workspace.RunLifecycle(ctx, name, func(view RunView) error {
		approved, err := approvedBundle(ctx, view)
		if err != nil {
			return err
		}
		// The file is kept only once the runtime is admitted, which is where a
		// caller first names it, so a run refused before then leaves no unnamed
		// file behind.
		return s.guard.WithPython(ctx, approved.area, approved.requirement, func(launch prerequisites.PythonLaunch, _ func() error) error {
			runtime := Runtime{Context: view.Identity(), Launch: launch, Bundle: approved.location, Area: approved.area, Material: material}
			if !request.RetainOutput {
				return call(ctx, runtime)
			}
			output, logs, directory, err := s.retain(ctx, view)
			if err != nil {
				return err
			}
			defer func() { _ = output.Close(ctx) }()
			runtime.Output, runtime.LogLocation, runtime.Logs, runtime.OutputRemediation = output, directory, logs, runOutputRemediation
			return call(ctx, runtime)
		})
	})
}

// runOutputRemediation points an adapter failure at the one file a bounded run
// keeps, which has no attempt log to sit beside.
const runOutputRemediation = "read the adapter output retained in this run's " + runOutputName

// retain opens the file this run's adapter output is kept in, under an
// identity of its own, and names it relative to the state root. The directory
// exists before the call, so the path a result names is one an operator can
// open while the run is still going.
func (s Service) retain(ctx context.Context, view RunView) (*operationstore.AdapterOutput, []string, string, error) {
	area := view.Runs()
	identity, err := reconciliation.AllocateRunID(s.options.Entropy, func(candidate string) bool {
		_, found, _ := area.Read(ctx, path.Join(candidate, runOutputName), 1)
		return found
	})
	if err != nil {
		return nil, nil, "", err
	}
	target := path.Join(identity, runOutputName)
	if err := area.EnsureDirectory(ctx, identity); err != nil {
		return nil, nil, "", abandonRun(ctx, area, identity, "", err)
	}
	// Exclusive creation is what proves the name is this run's own, and it
	// leaves the file an operator was told about already there to open.
	if err := area.WriteExclusive(ctx, target, nil); err != nil {
		return nil, nil, "", abandonRun(ctx, area, identity, target, err)
	}
	directory := area.Location()
	if directory != "" {
		directory = path.Join(directory, identity)
	}
	logs := []string{}
	if reference := area.Reference(); reference != "" {
		logs = append(logs, path.Join(reference, target))
	}
	return s.options.Operations(area).OpenAdapterOutput(ctx, target), logs, directory, nil
}

// abandonRun removes what a run made for a file it could not keep, since no
// result names either, and reports the failure that stopped the run. A file
// whose bytes landed before its publication failed goes first, and only while
// it is still as empty as this run wrote it, so the directory can go after it.
// The removals outlive an interrupt, which can land between the directory and
// its file or once the file landed; a removal that fails leaves that failure as
// it was.
func abandonRun(ctx context.Context, area operationstore.Area, identity, file string, err error) error {
	unstoppable := context.WithoutCancel(ctx)
	if file != "" {
		_ = area.RemoveRecord(unstoppable, file, nil)
	}
	_ = area.RemoveDirectory(unstoppable, identity)
	return err
}

// MaterialRequest names the context whose Secret declarations one bounded
// consumer needs opened, and nothing else: a consumer that runs no adapter
// needs neither the approved bundle nor a run identity.
type MaterialRequest struct {
	ContextName string
	Secrets     []string
}

// WithMaterial opens exactly the declared Secrets for the length of one call
// and releases them as it returns. It takes no store lock, registers no
// operation and publishes nothing, so an explicit access command reads a
// coherent set of material without waiting on, or resembling, an operation.
func (s Service) WithMaterial(ctx context.Context, request MaterialRequest, use func(context.Context, map[string]secrets.Material) error) error {
	if err := s.available(ctx); err != nil {
		return err
	}
	if use == nil {
		return failure("lifecycle.state", "a bounded material request carries no consumer", "")
	}
	name, err := s.resolve(ctx, request.ContextName)
	if err != nil {
		return err
	}
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
	return use(ctx, material)
}

// lendAttempts bounds how often one bounded operation binds its Secrets, once
// and again each time a collection released its binding before it reopened
// it.
const lendAttempts = 3

// lend freezes exactly the Secret versions one bounded operation reads. The
// binding is transient: it exists so the operation reads a coherent set, and
// is released as soon as the call returns. A binding names no consumer, so a
// lifecycle collection that listed this one before it was reopened may release
// it; a reopen that fails while the binding is no longer listed binds again,
// and any other failure, or a listing that fails, is reported.
func (s Service) lend(ctx context.Context, name string, references []string) (string, map[string]secrets.Material, error) {
	if len(references) == 0 {
		return "", map[string]secrets.Material{}, nil
	}
	for attempt := 1; ; attempt++ {
		result, err := s.binder.Bind(ctx, custody.BindRequest{ContextName: name, Names: references})
		if err != nil {
			return "", nil, err
		}
		request := custody.BindingRequest{ContextName: name, BindingID: result.ID}
		bound, err := s.binder.Reopen(ctx, request)
		if err == nil {
			material := make(map[string]secrets.Material, len(bound))
			for _, item := range bound {
				material[item.Version.Declaration.Name] = item.Material
			}
			return result.ID, material, nil
		}
		listed, listing := s.binder.Bindings(ctx, custody.BindingsRequest{ContextName: name})
		_, _ = s.binder.Release(ctx, request)
		if listing != nil || slices.Contains(listed, result.ID) || attempt == lendAttempts {
			return "", nil, err
		}
	}
}

// bundle is the approved execution boundary one operation runs inside: the
// area itself, where it sits on this host, and the requirement its runtime
// must satisfy. An operation opens it once and every attempt shares it,
// because opening it per attempt would ask the transaction to record what it
// has open while its own blocks are running.
type bundle struct {
	area        prerequisites.BundleArea
	location    prerequisites.BundleLocation
	requirement prerequisites.ExecutionRequirement
}

// approvedBundle opens the execution bundle the retained controller setup
// approved for this context, with the requirement its runtime must satisfy.
func approvedBundle(ctx context.Context, view View) (bundle, error) {
	receipt := view.Controller().State.Receipt
	if receipt.Definition == nil {
		return bundle{}, failure("controller.state",
			"the retained controller setup has no execution definition", "run bootwright setup")
	}
	if view.Controller().OpenBundle == nil {
		return bundle{}, failure("controller.state", "the approved execution bundle is unavailable", "run bootwright setup")
	}
	area, err := view.Controller().OpenBundle(ctx, receipt.CatalogDigest)
	if err != nil {
		return bundle{}, err
	}
	if area == nil {
		return bundle{}, failure("controller.state", "the approved execution bundle is missing", "run bootwright setup")
	}
	location, err := area.Location(ctx)
	if err != nil {
		return bundle{}, err
	}
	return bundle{area: area, location: location, requirement: receipt.Definition.Execution}, nil
}
