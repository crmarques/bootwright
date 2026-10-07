//go:build linux && amd64

package contextfs

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	p "github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// foundationResolution is the synthetic resolution with an execution
// foundation in the compiled shape, and a foundation qualified from it.
func foundationResolution(t *testing.T) (p.Definition, p.QualifiedFoundation) {
	t.Helper()
	base := syntheticResolution(t)
	bootstrap := *base.Bootstrap
	bootstrap.Execution = p.ExecutionRequirement{PythonExecutable: bootstrap.PythonExecutable, Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/usr/lib/sysimage/rpm/.rpm.lock",
		Files:   []p.InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: strings.Repeat("1", 64)}, {Path: "/usr/lib64/libgcc_s-15-20260722.so.1", SHA256: strings.Repeat("2", 64)}},
		Links:   []p.InstalledLink{{Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-15-20260722.so.1"}},
		Preload: []string{"/usr/lib64/libgcc_s-15-20260722.so.1"}}
	bootstrap, err := p.CanonicalBootstrap(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := p.NewResolvedDefinition(bootstrap, *base.Native)
	if err != nil {
		t.Fatal(err)
	}
	foundation := p.QualifiedFoundation{
		Execution: p.ExecutionRequirement{Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/usr/lib/sysimage/rpm/.rpm.lock",
			Files:   []p.InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: strings.Repeat("3", 64)}, {Path: "/usr/lib64/libgcc_s-15-20261001.so.1", SHA256: strings.Repeat("4", 64)}},
			Links:   []p.InstalledLink{{Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-15-20261001.so.1"}},
			Preload: []string{"/usr/lib64/libgcc_s-15-20261001.so.1"}},
		Packages: []p.FoundationBuild{{Name: "glibc", Build: "2.42-17.fc43", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2"}}, {Name: "libgcc", Build: "15.3.1-2.fc43", Files: []string{"/usr/lib64/libgcc_s-15-20261001.so.1"}}},
	}
	return definition, foundation
}

// A receipt records the execution foundation setup qualified, bound into its
// plan digest and read back unaliased; one whose foundation is not in its
// definition's shape, or that has no definition, is refused.
func TestAReceiptFoundationMustKeepItsDefinitionsShape(t *testing.T) {
	store, _ := fixture(t)
	scope := p.SetupContext{}
	definition, foundation := foundationResolution(t)
	value := syntheticControllerState(t, scope)
	value.Receipt.Definition = &definition
	value.Receipt.Foundation = &foundation
	value.Receipt.CatalogDigest = definition.CatalogDigest
	value.Receipt.Sources = slices.Clone(definition.Sources)
	var err error
	value.Receipt.PlanDigest, err = p.SetupPlanDigest(value.Host, value.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	unbound := value.Receipt
	unbound.Foundation = nil
	if digest, _ := p.SetupPlanDigest(value.Host, unbound); digest == value.Receipt.PlanDigest {
		t.Fatal("the plan digest does not bind the recorded foundation")
	}
	publishControllerState(t, store, scope, value)
	if err := store.ReadController(context.Background(), "", func(view p.StorageView) error {
		if !reflect.DeepEqual(view.State.Receipt.Foundation, &foundation) {
			t.Fatalf("the receipt read back %+v", view.State.Receipt.Foundation)
		}
		view.State.Receipt.Foundation.Execution.Files[0].SHA256 = strings.Repeat("5", 64)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*p.HostState){
		"a stale link": func(state *p.HostState) {
			state.Receipt.Foundation.Execution.Links[0].Target = "libgcc_s-15-20260722.so.1"
		},
		"another loader": func(state *p.HostState) { state.Receipt.Foundation.Execution.Loader = "/usr/lib64/ld.so" },
		"a short digest": func(state *p.HostState) { state.Receipt.Foundation.Execution.Files[1].SHA256 = "4" },
		"no attribution": func(state *p.HostState) { state.Receipt.Foundation.Packages = nil },
		"no definition":  func(state *p.HostState) { state.Receipt.Definition = nil },
		"an extra library": func(state *p.HostState) {
			state.Receipt.Foundation.Execution.Files = append(state.Receipt.Foundation.Execution.Files, p.InstalledFile{Path: "/usr/lib64/libm.so.6", SHA256: strings.Repeat("6", 64)})
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := cloneControllerState(value)
			change(&changed)
			changed.Receipt.PlanDigest, _ = p.SetupPlanDigest(changed.Host, changed.Receipt)
			changed, err := retainControllerSources(p.HostState{}, changed)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateControllerState(changed); err == nil {
				t.Fatal("an invalid receipt foundation was admitted")
			}
		})
	}
}
