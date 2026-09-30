package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// testBootstrap is the Python and Ansible resolution the harness's completed
// setup approves. Only its closure identity reaches the engine, so it is not
// held to the canonical form a controller record read enforces.
func testBootstrap() *prerequisites.BootstrapDefinition {
	source := func(id, url string) prerequisites.DependencySource {
		return prerequisites.DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
	}
	return &prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"},
		PythonIntent: "latest", AnsibleIntent: "latest", PythonVersion: "3.14.7", AnsibleVersion: "2.21.4",
		PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources: []prerequisites.DependencySource{
			source("python", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"),
			source("ansible", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"),
		},
		Wheels:           []prerequisites.BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible"}},
		AutomationDigest: testAutomaton,
		Execution: prerequisites.ExecutionRequirement{
			PythonExecutable: "python/bin/python3.14", Files: []prerequisites.InstalledFile{}, Links: []prerequisites.InstalledLink{}, Preload: []string{},
		},
		ExecutionPackages: []string{"glibc", "libgcc"},
	}
}

// movedBootstrap is the same resolution at a later ansible-core release, as a
// setup that resolved Python and Ansible again under the same automation
// publishes it.
func movedBootstrap() *prerequisites.BootstrapDefinition {
	moved := testBootstrap()
	moved.AnsibleVersion, moved.Wheels[0].Version = "2.21.5", "2.21.5"
	moved.Sources[1].URL = "https://files.pythonhosted.org/packages/ansible_core-2.21.5-py3-none-any.whl"
	moved.Sources[1].SHA256 = strings.Repeat("b", 64)
	return moved
}

func closureOf(t *testing.T, bootstrap *prerequisites.BootstrapDefinition) operationstore.Closure {
	t.Helper()
	digest, err := prerequisites.ClosureDigest(*bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	return operationstore.Closure{Digest: digest, Python: bootstrap.PythonVersion, Ansible: bootstrap.AnsibleVersion}
}

// approve replaces the bundle the host's completed setup approves, as a
// later setup does: another catalog digest naming another definition.
func approve(view *prerequisites.StorageView, catalog string, definition prerequisites.Definition) {
	definition.CatalogDigest = catalog
	view.State.Receipt.CatalogDigest, view.State.Receipt.Definition = catalog, &definition
}

// durable is everything a refused continuation must leave as it found it.
type durable struct {
	files             map[string][]byte
	evidence          []byte
	opened, binds     int
	guarded, executed int
}

func durableOf(h *harness) durable {
	h.guard.mutex.Lock()
	guarded := h.guard.calls
	h.guard.mutex.Unlock()
	h.capability.mutex.Lock()
	executed := len(h.capability.calls) + len(h.capability.destroys)
	h.capability.mutex.Unlock()
	return durable{
		files: h.workspace.area.clone().files, evidence: slices.Clone(h.workspace.evidence),
		opened: h.workspace.opened, binds: h.workspace.binds, guarded: guarded, executed: executed,
	}
}

func (d durable) equal(other durable) bool {
	return maps.EqualFunc(d.files, other.files, bytes.Equal) && bytes.Equal(d.evidence, other.evidence) &&
		d.opened == other.opened && d.binds == other.binds && d.guarded == other.guarded && d.executed == other.executed
}

// refusedUnchanged runs verb, which must refuse lifecycle.state with exactly
// message and remediation, and proves the refusal left every record, the
// evidence and the host as they were: no log restored, no state marked, no
// bundle opened and no effect.
func refusedUnchanged(t *testing.T, h *harness, verb reconciliation.Verb, message, remediation string) {
	t.Helper()
	before := durableOf(h)
	var err error
	if verb == reconciliation.Destroy {
		_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	} else {
		_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true})
	}
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("the continued %s reported %+v, want %q with %q", verb, reported, message, remediation)
	}
	if !before.equal(durableOf(h)) {
		t.Fatalf("the refused continuation of %s changed durable state or reached the host", verb)
	}
}

// failedApply leaves the harness's apply failed on its one block, which a
// fresh removal may supersede.
func failedApply(t *testing.T, h *harness) {
	t.Helper()
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := apply(h); err == nil {
		t.Fatal("the apply a continuation starts from did not fail")
	}
}

// unknownRemoval leaves the harness's removal of a completed apply unknown on
// its one block, which only a continuation may settle.
func unknownRemoval(t *testing.T, h *harness) {
	t.Helper()
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err == nil {
		t.Fatal("the removal a continuation starts from did not lose its outcome")
	}
	if operation, _ := durableOperation(t, h); operation.Verb != reconciliation.Destroy || operation.State != reconciliation.OperationUnknown {
		t.Fatalf("the removal is %s %s, not an unknown destroy", operation.Verb, operation.State)
	}
}

// Every registration records the closure of the bundle its effects run in,
// and a fresh verb runs under the bundle in hand: a removal registered after
// setup moved the closure records the moved one.
func TestARegistrationFreezesTheBundlesExecutionClosure(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := apply(h); err != nil {
		t.Fatal(err)
	}
	applied, _ := durableOperation(t, h)
	if applied.Version != operationstore.OperationVersion || applied.Closure == nil || *applied.Closure != closureOf(t, testBootstrap()) {
		t.Fatalf("the apply registered version %d with closure %+v", applied.Version, applied.Closure)
	}
	approve(&h.workspace.controller, strings.Repeat("c", 64), prerequisites.Definition{Bootstrap: movedBootstrap()})
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("a fresh removal under a moved closure: %v", err)
	}
	removal, _ := durableOperation(t, h)
	if removal.Verb != reconciliation.Destroy || removal.Closure == nil || *removal.Closure != closureOf(t, movedBootstrap()) {
		t.Fatalf("the removal registered closure %+v", removal.Closure)
	}
}

// A continuation of either verb refuses a bundle holding another Python and
// Ansible closure before it restores its log or marks its operation running,
// naming the releases and the build it registered under, and goes on once the
// closure it registered with is restored.
func TestAContinuationRefusesAMovedClosureWithNoEffect(t *testing.T) {
	const moved = "the approved execution bundle holds another Python and Ansible closure (Python 3.14.7, ansible-core 2.21.5) than the one this operation registered with"
	const restore = "restore the execution bundle of Python 3.14.7 and ansible-core 2.21.4 that bootwright devel (abcdef1) registered this operation with"
	for _, test := range []struct {
		verb        reconciliation.Verb
		start       func(*testing.T, *harness)
		remediation string
		continued   func(*harness) []string
	}{
		{reconciliation.Apply, failedApply, "destroy what this operation owns under the approved bundle, or " + restore, func(h *harness) []string { return h.capability.applies }},
		{reconciliation.Destroy, unknownRemoval, restore, func(h *harness) []string { return h.capability.observes }},
	} {
		t.Run(string(test.verb), func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			test.start(t, h)
			registered, _ := durableOperation(t, h)
			original := *h.workspace.controller.State.Receipt.Definition
			approve(&h.workspace.controller, strings.Repeat("c", 64), prerequisites.Definition{Bootstrap: movedBootstrap()})
			refusedUnchanged(t, h, test.verb, moved, test.remediation)
			h.workspace.controller.State.Receipt.CatalogDigest, h.workspace.controller.State.Receipt.Definition = strings.Repeat("b", 64), &original
			h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
			before := len(test.continued(h))
			result, err := func() (*OperationResult, error) {
				if test.verb == reconciliation.Destroy {
					return h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
				}
				return apply(h)
			}()
			if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) || len(test.continued(h)) == before {
				t.Fatalf("the continuation under the restored closure = %+v (%v)", result, err)
			}
		})
	}
}

// A closure is its digest, not its releases: a bundle that keeps Python
// 3.14.7 and ansible-core 2.21.4 but holds another interpreter build, another
// dependency wheel or another execution foundation is another closure, and a
// continuation of either verb refuses it with no effect.
func TestAContinuationRefusesAClosureMovedUnderTheSameReleases(t *testing.T) {
	const moved = "the approved execution bundle holds another Python and Ansible closure (Python 3.14.7, ansible-core 2.21.4) than the one this operation registered with"
	const restore = "restore the execution bundle of Python 3.14.7 and ansible-core 2.21.4 that bootwright devel (abcdef1) registered this operation with"
	for name, change := range map[string]func(*prerequisites.BootstrapDefinition){
		"another interpreter build": func(value *prerequisites.BootstrapDefinition) {
			value.Sources[0].URL = "https://github.com/astral-sh/python-build-standalone/releases/download/20260915/cpython-3.14.7%2B20260915-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"
			value.Sources[0].SHA256 = strings.Repeat("c", 64)
		},
		"another dependency wheel": func(value *prerequisites.BootstrapDefinition) {
			value.Sources = append(value.Sources, prerequisites.DependencySource{
				ID: "jinja2", URL: "https://files.pythonhosted.org/packages/jinja2-3.1.7-py3-none-any.whl", SHA256: strings.Repeat("d", 64), Bytes: 64,
			})
			value.Wheels = append(value.Wheels, prerequisites.BootstrapWheel{Name: "jinja2", Version: "3.1.7", SourceID: "jinja2"})
		},
		"another execution foundation": func(value *prerequisites.BootstrapDefinition) {
			value.Execution.Files = []prerequisites.InstalledFile{{Path: "/usr/lib64/libc.so.6", SHA256: strings.Repeat("5", 64)}}
		},
	} {
		for _, test := range []struct {
			verb        reconciliation.Verb
			start       func(*testing.T, *harness)
			remediation string
		}{
			{reconciliation.Apply, failedApply, "destroy what this operation owns under the approved bundle, or " + restore},
			{reconciliation.Destroy, unknownRemoval, restore},
		} {
			t.Run(name+"/"+string(test.verb), func(t *testing.T) {
				h := newHarness(t, "artifact-server-lab")
				test.start(t, h)
				bootstrap := testBootstrap()
				change(bootstrap)
				if closureOf(t, bootstrap).Digest == closureOf(t, testBootstrap()).Digest {
					t.Fatalf("%s left the closure digest as it was", name)
				}
				approve(&h.workspace.controller, strings.Repeat("c", 64), prerequisites.Definition{Bootstrap: bootstrap})
				refusedUnchanged(t, h, test.verb, moved, test.remediation)
			})
		}
	}
}

// A setup that solves only the native transaction again republishes the
// bundle under another catalog digest with the same Python and Ansible
// closure, and a continuation runs inside that bundle: the native packages
// are the host's, not the operation's.
func TestANativeOnlyResolveContinuesTheOperation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	registered, _ := durableOperation(t, h)
	resolved := strings.Repeat("d", 64)
	approve(&h.workspace.controller, resolved, prerequisites.Definition{
		Bootstrap: testBootstrap(), NativeRequirements: prerequisites.NativeRequirements{ContainerRuntime: true},
		Native: &prerequisites.NativeResolvedPlan{Format: "bootwright.native-plan-v1", Solver: "dnf5", SolverVersion: "5.2.0", AfterSHA256: strings.Repeat("e", 64)},
	})
	opened := []string{}
	h.workspace.controller.OpenBundle = func(_ context.Context, digest string) (prerequisites.BundleArea, error) {
		opened = append(opened, digest)
		return testBundle{}, nil
	}
	result, err := apply(h)
	if err != nil || result.Receipt.Operation != registered.ID || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the continuation over a native re-solve = %+v (%v)", result, err)
	}
	if !slices.Equal(opened, []string{resolved}) || !slices.Equal(h.capability.applies, []string{"artifact-server-lab", "artifact-server-lab"}) {
		t.Fatalf("the continuation opened %v and applied %v", opened, h.capability.applies)
	}
}

// A continuation of an operation an earlier build registered, whose record
// names no closure, refuses and names that build, since nothing says which
// closure its effects ran in. The record still serves a fresh verb: a removal
// supersedes the apply it describes under the bundle in hand.
func TestAContinuationRefusesAnEarlierVersionOperationRecord(t *testing.T) {
	const message = "this operation was registered by an earlier build that froze no Python and Ansible closure"
	const continued = "continue it with bootwright devel (abcdef1), which registered it"
	for _, test := range []struct {
		verb        reconciliation.Verb
		start       func(*testing.T, *harness)
		remediation string
	}{
		{reconciliation.Apply, failedApply, "destroy what this operation owns under this executable, or " + continued},
		{reconciliation.Destroy, unknownRemoval, continued},
	} {
		t.Run(string(test.verb), func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			test.start(t, h)
			registeredEarlier(t, h)
			refusedUnchanged(t, h, test.verb, message, test.remediation)
		})
	}
	h := newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	registeredEarlier(t, h)
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("a removal superseding an earlier build's apply = %+v (%v)", result, err)
	}
}

// Every build that froze no closure predates setup runs and refuses a
// controller directory that keeps them, so once setup kept a run the refusal
// of such an operation's continuation names no earlier build, whether or not
// the automation also moved. A removal under this executable supersedes an
// apply, and a removal that none may supersede leaves only the deletion of the
// context. An operation that froze its closure still names the build that
// registered it, which reads setup runs.
func TestAnEarlierVersionRecordNamesNoEarlierBuildOnceSetupKeepsRuns(t *testing.T) {
	const message = "this operation was registered by an earlier build that froze no Python and Ansible closure, " +
		"and bootwright devel (abcdef1) cannot read this host's controller directory, which now keeps setup runs"
	for _, test := range []struct {
		verb        reconciliation.Verb
		start       func(*testing.T, *harness)
		remediation string
	}{
		{reconciliation.Apply, failedApply, "destroy what this operation owns under this executable"},
		{reconciliation.Destroy, unknownRemoval, "delete the context with bootwright context delete --name " + testContextName + " --purge --allow-orphans, which abandons what it may still own"},
	} {
		for _, moved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s with moved automation %t", test.verb, moved), func(t *testing.T) {
				h := newHarness(t, "artifact-server-lab")
				test.start(t, h)
				registeredEarlier(t, h)
				h.workspace.controller.SetupRuns = true
				if moved {
					h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
				}
				refusedUnchanged(t, h, test.verb, message, test.remediation)
			})
		}
	}
	h := newHarness(t, "artifact-server-lab")
	unknownRemoval(t, h)
	h.workspace.controller.SetupRuns = true
	h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
	refusedUnchanged(t, h, reconciliation.Destroy, "this executable's automation differs from the one this operation froze",
		"install bootwright devel (abcdef1), which registered this operation, and run bootwright setup")
	h = newHarness(t, "artifact-server-lab")
	failedApply(t, h)
	registeredEarlier(t, h)
	h.workspace.controller.SetupRuns = true
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: testContextName, SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("a removal superseding an earlier build's apply beside setup runs = %+v (%v)", result, err)
	}
}

// registeredEarlier rewrites the current operation's record as a build before
// the closure was frozen published it: version 1 and no closure.
func registeredEarlier(t *testing.T, h *harness) {
	t.Helper()
	target := path.Join(currentOperation(t, h), "operation.json")
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	var record operationstore.Operation
	if err := json.Unmarshal(h.workspace.area.files[target], &record); err != nil {
		t.Fatal(err)
	}
	record.Version, record.Closure = 1, nil
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	h.workspace.area.files[target] = append(data, '\n')
}

// A changed automation digest is refused as before, and the refusal now names
// the build a continuation needs.
func TestAChangedAutomationRefusalNamesTheExecutable(t *testing.T) {
	for _, test := range []struct {
		verb        reconciliation.Verb
		start       func(*testing.T, *harness)
		remediation string
	}{
		{reconciliation.Apply, failedApply, "destroy what this operation owns under this executable, or install bootwright devel (abcdef1), which registered it"},
		{reconciliation.Destroy, unknownRemoval, "install bootwright devel (abcdef1), which registered this operation, and run bootwright setup"},
	} {
		t.Run(string(test.verb), func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			test.start(t, h)
			h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
			refusedUnchanged(t, h, test.verb, "this executable's automation differs from the one this operation froze", test.remediation)
		})
	}
}

// A fresh apply killed at each write of its registration, or just after it,
// replays under the closure it froze. Under the same bundle the retry
// converges and every record names that closure. Under a moved bundle, a
// registration that landed refuses its continuation with no effect, and one
// that did not is a fresh apply again, registering the moved closure.
func TestARegistrationKilledAtEachWriteReplaysUnderItsClosure(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		point      string
		registered bool
	}{
		{"write <op>/plan.json#1", false}, {"write <op>/operation.json#1", false}, {"replace index.json#1", false},
		{"append <op>/logs/operation.jsonl#1", true}, {"replace <op>/blocks/a/state.json#1", true},
	} {
		point := test.point
		for _, move := range []bool{false, true} {
			rig := newKillRig(t)
			killed := &killPoints{counts: map[string]int{}, key: point}
			rig.arm(killed)
			if err := killInvoke(ctx, rig.harness.service, reconciliation.Apply); err == nil || killed.snapshot == nil {
				t.Fatalf("the apply was not killed at %s: %v", point, killed.keys)
			}
			snapshot := killed.snapshot
			service := rig.over(snapshot)
			want := closureOf(t, testBootstrap())
			registered := killRest(snapshot.workspace) != "removed"
			if registered != test.registered {
				t.Fatalf("killed at %s the context rests %s", point, killRest(snapshot.workspace))
			}
			if move {
				approve(&snapshot.workspace.controller, strings.Repeat("c", 64), prerequisites.Definition{Bootstrap: movedBootstrap()})
				want = closureOf(t, movedBootstrap())
			}
			if move && registered {
				files := snapshot.workspace.area.clone().files
				err := killInvoke(ctx, service, reconciliation.Apply)
				if reported := diagnostics.Of(err); len(reported) != 1 || !strings.HasPrefix(reported[0].Message, "the approved execution bundle holds another Python and Ansible closure") {
					t.Fatalf("killed at %s, the continuation under a moved closure reported %+v", point, reported)
				}
				if !maps.EqualFunc(files, snapshot.workspace.area.clone().files, bytes.Equal) || snapshot.host.effectCount(reconciliation.Apply, "a") != 0 {
					t.Fatalf("killed at %s, the refused continuation changed its records or reached the host", point)
				}
				continue
			}
			for retry := 0; retry < killBoundFreshApply && killRest(snapshot.workspace) != "applied"; retry++ {
				_ = killInvoke(ctx, service, reconciliation.Apply)
			}
			if rest := killRest(snapshot.workspace); rest != "applied" {
				t.Fatalf("killed at %s (moved %t), %d applies leave the context %s", point, move, killBoundFreshApply, rest)
			}
			if failures := killReadBack(snapshot.workspace); len(failures) != 0 {
				t.Fatalf("killed at %s (moved %t): %v", point, move, failures)
			}
			store := operationstore.New(snapshot.workspace.area, killClock)
			index, err := store.Index(ctx)
			if err != nil {
				t.Fatal(err)
			}
			operation, err := store.ReadOperation(ctx, index.Current)
			if err != nil || operation.Version != operationstore.OperationVersion || operation.Closure == nil || *operation.Closure != want {
				t.Fatalf("killed at %s (moved %t), the applied operation reads %+v (%v)", point, move, operation, err)
			}
		}
	}
}
