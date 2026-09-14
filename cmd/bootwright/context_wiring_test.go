package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"github.com/crmarques/bootwright/internal/workspace/selectionfs"
)

func testRepository(root string) *contextfs.Store {
	return contextfs.New(contextfs.Options{Root: root, Owner: &contextfs.Ownership{UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}})
}

func testContextWiring(t *testing.T, root string) serviceDependencies {
	t.Helper()
	home := filepath.Dir(root)
	return serviceDependencies{
		Selection: selectionfs.New(selectionfs.Options{UID: os.Getuid(), GID: os.Getgid(), Home: home}),
		Operator:  testMaterialOperator{home: home},
	}
}

type testMaterialOperator struct{ home string }

func (o testMaterialOperator) FileIdentity(ctx context.Context) (secretmaterial.FileIdentity, error) {
	return secretmaterial.FileIdentity{UID: os.Getuid(), Home: o.home}, ctx.Err()
}

func isolatedServices(t *testing.T) cli.Services {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	return testServices(t, repository, root)
}

// testServices binds the complete graph to an isolated store, exactly as the
// composition root does for a real invocation.
func testServices(t *testing.T, repository *contextfs.Store, root string) cli.Services {
	t.Helper()
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace = repository, repository
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository,
		Inputs:    contexts.Inputs{Repository: repository, Selection: deps.Selection},
		Host:      testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer,
		Capabilities: testLifecycleCapabilities{},
	}
	return assembleServices(deps)
}

// The lifecycle harness binds the real context store with substituted host,
// execution and capability ports, so a journey exercises the durable boundary
// without a controller, a container runtime or a second host.
type testLifecycleHost struct{}

func (testLifecycleHost) Identity(ctx context.Context) (controller.InstalledHostIdentity, error) {
	return controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
}

type testLifecycleGuard struct{}

func (testLifecycleGuard) WithPython(_ context.Context, _ prerequisites.BundleArea, _ prerequisites.ExecutionRequirement, use func(prerequisites.PythonLaunch, func() error) error) error {
	return use(prerequisites.PythonLaunch{Loader: "/loader"}, func() error { return nil })
}

type testLifecyclePresenter struct{}

func (testLifecyclePresenter) PresentLifecyclePlan(context.Context, lifecycle.PlanResult) error {
	return nil
}

type testLifecycleCapabilities struct{}

func (testLifecycleCapabilities) Kinds() []string { return []string{"ArtifactServer"} }

func (testLifecycleCapabilities) Resolve(string, string) (lifecycle.Capability, bool) {
	return nil, false
}
