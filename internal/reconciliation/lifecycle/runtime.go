package lifecycle

import (
	"context"
	"path"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

// RunOutputName is what one bounded run retains beside nothing else: it
// publishes no structured events, so there is no log for its output to sit
// next to.
const RunOutputName = "run.output"

// RuntimeRequest names the context whose approved bundle a bounded operation
// runs inside, and the Secret declarations that operation reads.
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
	// The material is read first, in a keyring session that ends before the
	// run takes the context, so no keyring session stays open while the
	// adapter runs.
	material, err := s.lend(ctx, name, request.Secrets)
	if err != nil {
		return err
	}
	defer clearMaterial(material)
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
const runOutputRemediation = "read the adapter output retained in this run's " + RunOutputName

// retain opens the file this run's adapter output is kept in, under an
// identity of its own, and names it relative to the state root. The run view
// opens the run's directory, making room among the runs the area keeps, and
// that directory exists before the call, so the path a result names is one an
// operator can open while the run is still going.
func (s Service) retain(ctx context.Context, view RunView) (*operationstore.AdapterOutput, []string, string, error) {
	area := view.Runs()
	identity, err := reconciliation.AllocateRunID(s.options.Entropy, func(candidate string) bool {
		_, found, _ := area.Read(ctx, path.Join(candidate, RunOutputName), 1)
		return found
	})
	if err != nil {
		return nil, nil, "", err
	}
	target := path.Join(identity, RunOutputName)
	if err := view.OpenRun(ctx, identity); err != nil {
		return nil, nil, "", err
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
// and clears them as it returns. It holds the store's shared lock only while
// it reads, registers no operation and publishes nothing, so an explicit
// access command reads a coherent set of material without waiting on, or
// resembling, an operation.
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
	material, err := s.lend(ctx, name, request.Secrets)
	if err != nil {
		return err
	}
	defer clearMaterial(material)
	return use(ctx, material)
}

// lend reads the current version of exactly the Secrets one bounded call
// needs, in one keyring session, and binds nothing: a bounded call publishes
// no binding and reserves no identity, so a loop of them leaves the keyring as
// it found it and a registration's collection has nothing of it to release.
// The caller clears what it was lent when the call returns, a call ended by
// cancellation included.
func (s Service) lend(ctx context.Context, name string, references []string) (map[string]secrets.Material, error) {
	if len(references) == 0 {
		return map[string]secrets.Material{}, nil
	}
	current, err := s.binder.ReadCurrent(ctx, custody.ReadCurrentRequest{ContextName: name, Names: references})
	if err != nil {
		return nil, err
	}
	material := make(map[string]secrets.Material, len(current))
	for _, item := range current {
		material[item.Version.Declaration.Name] = item.Material
	}
	return material, nil
}

// bundle is the approved execution boundary one operation runs inside: the
// area itself, where it sits on this host, the requirement its runtime must
// satisfy and the context whose setup approved it. An operation opens it once
// and every attempt shares it, because opening it per attempt would ask the
// transaction to record what it has open while its own blocks are running.
type bundle struct {
	context     string
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
	return bundle{context: view.Identity().Name, area: area, location: location, requirement: receipt.Definition.Execution}, nil
}
