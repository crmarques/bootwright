// Package workspacecontract is the shared contract suite for
// lifecycle.Workspace. Every implementation's tests run it, the in-memory
// doubles included, so a double cannot accept what the Workspace adapter
// refuses. No production package imports it.
package workspacecontract

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// Subject is one fresh workspace holding the ready context Context, with
// empty operation and run areas, on a host whose completed controller setup
// identifies it as Host, names its approved bundle, binds no context and
// retains no dependency.
type Subject struct {
	Workspace lifecycle.Workspace
	Context   string
	Host      controller.InstalledHostIdentity
}

// Within provides one fresh Subject and fails the test when it cannot.
type Within func(t *testing.T) Subject

const bound = 64

// Verify holds one implementation to every clause, each against its own
// workspace.
func Verify(t *testing.T, within Within) {
	t.Helper()
	for _, clause := range []struct {
		name  string
		check func(*testing.T, Subject)
	}{
		{"each entry point refuses a context it does not hold", eachEntryPointRefusesAContextItDoesNotHold},
		{"each entry point refuses a missing callback", eachEntryPointRefusesAMissingCallback},
		{"an inspection writes nothing", anInspectionWritesNothing},
		{"a bounded run writes only its run area", aBoundedRunWritesOnlyItsRunArea},
		{"a transaction's records are what a later view reads", aTransactionsRecordsAreWhatALaterViewReads},
		{"evidence is published whole and read back as a copy", evidenceIsPublishedWholeAndReadBackAsACopy},
		{"a capability ends with its callback", aCapabilityEndsWithItsCallback},
		{"a binding is established once and revalidated exactly", aBindingIsEstablishedOnceAndRevalidatedExactly},
		{"reservations are the context's own and what a later view holds", reservationsAreTheContextsOwn},
		{"each view names the context it holds", eachViewNamesTheContextItHolds},
		{"a client area opens under a reservation and reopens sealed read-only", aClientAreaOpensUnderAReservation},
		{"retained dependencies are kept whole and never replaced", retainedDependenciesAreKeptWhole},
		{"a secret area is lent for the held context only inside its transaction", aSecretAreaIsLentOnlyInsideItsTransaction},
	} {
		t.Run(clause.name, func(t *testing.T) {
			subject := within(t)
			if subject.Workspace == nil || subject.Context == "" || !subject.Host.Valid() {
				t.Fatal("the runner provided no complete workspace")
			}
			clause.check(t, subject)
		})
	}
}

func eachEntryPointRefusesAContextItDoesNotHold(t *testing.T, s Subject) {
	ctx := context.Background()
	for _, name := range []string{"", "absent", "Not A Name"} {
		called := false
		for call, err := range map[string]error{
			"ReadLifecycle":   s.Workspace.ReadLifecycle(ctx, name, func(lifecycle.View) error { called = true; return nil }),
			"RunLifecycle":    s.Workspace.RunLifecycle(ctx, name, func(lifecycle.RunView) error { called = true; return nil }),
			"MutateLifecycle": s.Workspace.MutateLifecycle(ctx, name, func(lifecycle.Transaction) error { called = true; return nil }),
		} {
			if err == nil {
				t.Fatalf("%s of context %q succeeded", call, name)
			}
		}
		if called {
			t.Fatalf("a callback ran for context %q", name)
		}
	}
}

func eachEntryPointRefusesAMissingCallback(t *testing.T, s Subject) {
	ctx := context.Background()
	for call, err := range map[string]error{
		"ReadLifecycle":   s.Workspace.ReadLifecycle(ctx, s.Context, nil),
		"RunLifecycle":    s.Workspace.RunLifecycle(ctx, s.Context, nil),
		"MutateLifecycle": s.Workspace.MutateLifecycle(ctx, s.Context, nil),
	} {
		if err == nil {
			t.Fatalf("%s without a callback succeeded", call)
		}
	}
}

func anInspectionWritesNothing(t *testing.T, s Subject) {
	read(t, s, func(view lifecycle.View) {
		refusesEveryWrite(t, view.Operations(), "an inspection")
		lists(t, view.Operations())
	})
}

func aBoundedRunWritesOnlyItsRunArea(t *testing.T, s Subject) {
	ctx := context.Background()
	run(t, s, func(view lifecycle.RunView) {
		refusesEveryWrite(t, view.Operations(), "a bounded run's operation area")
		succeeds(t, view.Runs().WriteExclusive(ctx, "r.json", []byte("output\n")), "a bounded run's output")
		holds(t, view.Runs(), "r.json", "output\n")
	})
	run(t, s, func(view lifecycle.RunView) {
		holds(t, view.Runs(), "r.json", "output\n")
		lists(t, view.Operations())
	})
}

func aTransactionsRecordsAreWhatALaterViewReads(t *testing.T, s Subject) {
	ctx := context.Background()
	mutate(t, s, func(tx lifecycle.Transaction) {
		succeeds(t, tx.Operations().WriteExclusive(ctx, "r.json", []byte("record\n")), "an operation record")
		succeeds(t, tx.Operations().Append(ctx, "log.jsonl", []byte("one\n")), "an operation log")
		holds(t, tx.Operations(), "r.json", "record\n")
	})
	read(t, s, func(view lifecycle.View) {
		holds(t, view.Operations(), "r.json", "record\n")
		holds(t, view.Operations(), "log.jsonl", "one\n")
	})
	run(t, s, func(view lifecycle.RunView) {
		holds(t, view.Operations(), "r.json", "record\n")
	})
}

func evidenceIsPublishedWholeAndReadBackAsACopy(t *testing.T, s Subject) {
	ctx := context.Background()
	var initial []byte
	read(t, s, func(view lifecycle.View) { initial = view.Evidence() })
	next := []byte("{\"evidence\":\"next\"}\n")
	mutate(t, s, func(tx lifecycle.Transaction) {
		evidence(t, tx, initial, "the transaction's evidence")
		for _, empty := range [][]byte{nil, {}} {
			if err := tx.PublishEvidence(ctx, empty); err == nil {
				t.Fatal("empty evidence was published")
			}
		}
		evidence(t, tx, initial, "the evidence after a refused publication")
		succeeds(t, tx.PublishEvidence(ctx, next), "an evidence publication")
		evidence(t, tx, next, "the transaction's own publication")
		changes(tx)
		evidence(t, tx, next, "the evidence after its copy changed")
	})
	read(t, s, func(view lifecycle.View) {
		evidence(t, view, next, "a later view's evidence")
		changes(view)
		evidence(t, view, next, "a later view's evidence after its copy changed")
	})
}

func aCapabilityEndsWithItsCallback(t *testing.T, s Subject) {
	ctx := context.Background()
	escaped := map[string]operationstore.Area{}
	var client prerequisites.BundleArea
	read(t, s, func(view lifecycle.View) { escaped["an inspection's operation area"] = view.Operations() })
	run(t, s, func(view lifecycle.RunView) { escaped["a bounded run's run area"] = view.Runs() })
	mutate(t, s, func(tx lifecycle.Transaction) {
		escaped["a transaction's operation area"] = tx.Operations()
		client = clientArea(t, tx, closure)
	})
	for name, area := range escaped {
		if _, _, err := area.Read(ctx, "r.json", bound); err == nil {
			t.Fatalf("%s read after its callback returned", name)
		}
		if err := area.WriteExclusive(ctx, "r.json", []byte("late\n")); err == nil {
			t.Fatalf("%s wrote after its callback returned", name)
		}
	}
	if _, err := client.Entries(ctx); err == nil {
		t.Fatal("a client area listed after its transaction returned")
	}
	if err := client.Write(ctx, "late.json", []byte("{}\n"), false); err == nil {
		t.Fatal("a client area wrote after its transaction returned")
	}
	read(t, s, func(view lifecycle.View) { lists(t, view.Operations()) })
	run(t, s, func(view lifecycle.RunView) { lists(t, view.Runs()) })
}

func aBindingIsEstablishedOnceAndRevalidatedExactly(t *testing.T, s Subject) {
	ctx := context.Background()
	foreign, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"fedcba9876543210fedcba9876543210", "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := s.Host.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	binding := prerequisites.ControllerBinding{Context: s.Context, Machine: "controller", HostDigest: digest}
	mutate(t, s, func(tx lifecycle.Transaction) {
		refuses(t, tx.Bind(ctx, "", s.Host), "a binding naming no Machine")
		refuses(t, tx.Bind(ctx, "controller", foreign), "a binding of another host")
		bindings(t, tx, s.Context)
		succeeds(t, tx.Bind(ctx, "controller", s.Host), "the first binding")
		succeeds(t, tx.Bind(ctx, "controller", s.Host), "the same binding again")
		refuses(t, tx.Bind(ctx, "replacement", s.Host), "a binding of another Machine")
		bindings(t, tx, s.Context, binding)
	})
	read(t, s, func(view lifecycle.View) { bindings(t, view, s.Context, binding) })
	mutate(t, s, func(tx lifecycle.Transaction) {
		refuses(t, tx.Bind(ctx, "replacement", s.Host), "a later binding of another Machine")
		refuses(t, tx.Bind(ctx, "controller", foreign), "a later binding of another host")
		succeeds(t, tx.Bind(ctx, "controller", s.Host), "a later revalidation")
		bindings(t, tx, s.Context, binding)
	})
}

func reservationsAreTheContextsOwn(t *testing.T, s Subject) {
	ctx := context.Background()
	claim := []prerequisites.HostReservation{{
		Context: s.Context, Kind: "proxy", Service: "lab-proxy",
		Keys: []string{"socket:192.0.2.1:3128", "unit:bootwright-proxy"},
	}}
	foreign := slices.Clone(claim)
	foreign[0].Context = "foreign"
	mutate(t, s, func(tx lifecycle.Transaction) {
		refuses(t, tx.Reserve(ctx, foreign), "a reservation for another context")
		reserves(t, tx, s.Context)
		succeeds(t, tx.Reserve(ctx, claim), "a reservation")
		reserves(t, tx, s.Context, claim...)
	})
	read(t, s, func(view lifecycle.View) { reserves(t, view, s.Context, claim...) })
	mutate(t, s, func(tx lifecycle.Transaction) {
		succeeds(t, tx.ReleaseReservations(ctx), "a release")
		reserves(t, tx, s.Context)
	})
	read(t, s, func(view lifecycle.View) { reserves(t, view, s.Context) })
}

func eachViewNamesTheContextItHolds(t *testing.T, s Subject) {
	var identities []lifecycle.ContextIdentity
	var inputs []any
	read(t, s, func(view lifecycle.View) {
		identities, inputs = append(identities, view.Identity()), append(inputs, view.Inputs())
	})
	run(t, s, func(view lifecycle.RunView) {
		identities, inputs = append(identities, view.Identity()), append(inputs, view.Inputs())
	})
	mutate(t, s, func(tx lifecycle.Transaction) {
		identities, inputs = append(identities, tx.Identity()), append(inputs, tx.Inputs())
	})
	for index, identity := range identities {
		if identity.Name != s.Context || identity.Mode != "ready" || identity.Revision == "" || identity != identities[0] {
			t.Fatalf("view %d names %+v, want the ready context %s at one revision", index, identity, s.Context)
		}
		if !reflect.DeepEqual(inputs[index], inputs[0]) {
			t.Fatalf("view %d reads inputs %+v, another %+v", index, inputs[index], inputs[0])
		}
	}
}

// closure is the client closure the suite publishes: a digest no setup names.
var closure = strings.Repeat("e", 64)

func aClientAreaOpensUnderAReservation(t *testing.T, s Subject) {
	ctx := context.Background()
	mutate(t, s, func(tx lifecycle.Transaction) {
		approved := tx.Controller().State.Receipt.CatalogDigest
		if len(approved) != 64 || approved == closure {
			t.Fatalf("the completed setup names approved bundle %q", approved)
		}
		for what, refused := range map[string]string{"no digest": "not-a-digest", "the approved setup bundle": approved} {
			if area, err := tx.ClientArea(ctx, refused); err == nil {
				t.Fatalf("a client area named by %s opened %v", what, area)
			}
		}
		refuses(t, tx.SealClientArea(ctx, closure), "sealing a client area never opened")
		area := clientArea(t, tx, closure)
		succeeds(t, area.Write(ctx, "manifest.json", []byte("{}\n"), false), "a client file")
		publishes(t, area, "manifest.json", "{}\n")
		succeeds(t, tx.SealClientArea(ctx, closure), "sealing the client area")
		succeeds(t, tx.SealClientArea(ctx, closure), "sealing it again")
		refuses(t, area.Write(ctx, "late.json", []byte("{}\n"), false), "a write into a sealed client area")
	})
	read(t, s, func(view lifecycle.View) {
		if !slices.Contains(view.Controller().Areas, prerequisites.HeldArea{ID: closure}) {
			t.Fatalf("held areas = %+v, want the client area %s", view.Controller().Areas, closure)
		}
	})
	mutate(t, s, func(tx lifecycle.Transaction) {
		area := clientArea(t, tx, closure)
		publishes(t, area, "manifest.json", "{}\n")
		refuses(t, area.Write(ctx, "late.json", []byte("{}\n"), false), "a write into a reopened sealed client area")
		succeeds(t, tx.SealClientArea(ctx, closure), "sealing a sealed client area")
	})
}

func retainedDependenciesAreKeptWhole(t *testing.T, s Subject) {
	ctx := context.Background()
	resolution := syntheticResolution(t)
	source := prerequisites.DependencySource{ID: "client-tool", URL: "https://tools.example.test/client.tar.gz", SHA256: strings.Repeat("a", 64), Bytes: 64}
	replaced, incomplete, altered := source, prerequisites.CloneDefinition(resolution), prerequisites.CloneDefinition(resolution)
	replaced.SHA256 = strings.Repeat("b", 64)
	incomplete.Native = nil
	altered.PythonVersion = "3.14.8"
	mutate(t, s, func(tx lifecycle.Transaction) {
		succeeds(t, tx.RetainDependencies(ctx, nil, []prerequisites.DependencySource{source}), "retaining a source")
		succeeds(t, tx.RetainDependencies(ctx, nil, []prerequisites.DependencySource{source}), "retaining it again")
		refuses(t, tx.RetainDependencies(ctx, nil, []prerequisites.DependencySource{replaced}), "a source replaced under its identity")
		refuses(t, tx.RetainDependencies(ctx, &incomplete, incomplete.Sources), "an incomplete resolution")
		refuses(t, tx.RetainDependencies(ctx, &resolution, nil), "a resolution without its sources")
		retains(t, tx, []prerequisites.DependencySource{source})
		succeeds(t, tx.RetainDependencies(ctx, &resolution, resolution.Sources), "retaining a resolution with its sources")
		succeeds(t, tx.RetainDependencies(ctx, &resolution, resolution.Sources), "retaining it again")
		refuses(t, tx.RetainDependencies(ctx, &altered, altered.Sources), "a resolution replaced under its identity")
		retains(t, tx, append([]prerequisites.DependencySource{source}, resolution.Sources...), resolution)
	})
	read(t, s, func(view lifecycle.View) {
		retains(t, view, append([]prerequisites.DependencySource{source}, resolution.Sources...), resolution)
	})
}

func aSecretAreaIsLentOnlyInsideItsTransaction(t *testing.T, s Subject) {
	ctx := context.Background()
	var escaped lifecycle.Transaction
	mutate(t, s, func(tx lifecycle.Transaction) {
		escaped = tx
		refuses(t, tx.Secrets(ctx, nil), "a secret loan without a callback")
		lent := 0
		succeeds(t, tx.Secrets(ctx, func(selected secretstore.Context, _ secretstore.Area) error {
			lent++
			if selected.Name != s.Context || selected.Mode != "ready" || selected.Revision != tx.Identity().Revision {
				t.Fatalf("a secret loan names context %+v, want %+v", selected, tx.Identity())
			}
			return nil
		}), "a secret loan")
		if lent != 1 {
			t.Fatalf("a secret loan called back %d times", lent)
		}
	})
	called := false
	if err := escaped.Secrets(ctx, func(secretstore.Context, secretstore.Area) error { called = true; return nil }); err == nil || called {
		t.Fatalf("a secret loan after its transaction returned = %v, called back %t", err, called)
	}
}

func read(t *testing.T, s Subject, use func(lifecycle.View)) {
	t.Helper()
	called := false
	if err := s.Workspace.ReadLifecycle(context.Background(), s.Context, func(view lifecycle.View) error {
		called = true
		use(view)
		return nil
	}); err != nil || !called {
		t.Fatalf("a lifecycle read failed (%v) or never called back", err)
	}
}

func run(t *testing.T, s Subject, use func(lifecycle.RunView)) {
	t.Helper()
	called := false
	if err := s.Workspace.RunLifecycle(context.Background(), s.Context, func(view lifecycle.RunView) error {
		called = true
		use(view)
		return nil
	}); err != nil || !called {
		t.Fatalf("a bounded run failed (%v) or never called back", err)
	}
}

func mutate(t *testing.T, s Subject, use func(lifecycle.Transaction)) {
	t.Helper()
	called := false
	if err := s.Workspace.MutateLifecycle(context.Background(), s.Context, func(tx lifecycle.Transaction) error {
		called = true
		use(tx)
		return nil
	}); err != nil || !called {
		t.Fatalf("a lifecycle mutation failed (%v) or never called back", err)
	}
}

func refusesEveryWrite(t *testing.T, area operationstore.Area, what string) {
	t.Helper()
	ctx := context.Background()
	for call, err := range map[string]error{
		"EnsureDirectory": area.EnsureDirectory(ctx, "op"),
		"WriteExclusive":  area.WriteExclusive(ctx, "r.json", []byte("x\n")),
		"Replace":         area.Replace(ctx, "r.json", []byte("x\n"), nil),
		"Append":          area.Append(ctx, "log.jsonl", []byte("x\n")),
		"Sync":            area.Sync(ctx, ""),
	} {
		if err == nil {
			t.Fatalf("%s accepted %s", what, call)
		}
	}
}

func evidence(t *testing.T, view lifecycle.View, want []byte, what string) {
	t.Helper()
	if got := view.Evidence(); !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", what, got, want)
	}
}

func changes(view lifecycle.View) {
	if held := view.Evidence(); len(held) != 0 {
		held[0] ^= 0xff
	}
}

func bindings(t *testing.T, view lifecycle.View, context string, want ...prerequisites.ControllerBinding) {
	t.Helper()
	held := slices.DeleteFunc(slices.Clone(view.Controller().State.Bindings), func(binding prerequisites.ControllerBinding) bool {
		return binding.Context != context
	})
	if !slices.Equal(held, want) {
		t.Fatalf("bindings of %s = %+v, want %+v", context, held, want)
	}
}

func reserves(t *testing.T, view lifecycle.View, context string, want ...prerequisites.HostReservation) {
	t.Helper()
	held := slices.DeleteFunc(slices.Clone(view.Controller().State.Reservations), func(reservation prerequisites.HostReservation) bool {
		return reservation.Context != context
	})
	if !slices.EqualFunc(held, want, func(x, y prerequisites.HostReservation) bool {
		return x.Context == y.Context && x.Kind == y.Kind && x.Service == y.Service && x.Shared == y.Shared && slices.Equal(x.Keys, y.Keys)
	}) {
		t.Fatalf("reservations of %s = %+v, want %+v", context, held, want)
	}
}

func clientArea(t *testing.T, tx lifecycle.Transaction, id string) prerequisites.BundleArea {
	t.Helper()
	area, err := tx.ClientArea(context.Background(), id)
	if err != nil || area == nil {
		t.Fatalf("client area %s = %v (%v)", id, area, err)
	}
	return area
}

func publishes(t *testing.T, area prerequisites.BundleArea, name, want string) {
	t.Helper()
	if data, err := area.Read(context.Background(), name, bound); err != nil || string(data) != want {
		t.Fatalf("client file %s = %q (%v), want %q", name, data, err, want)
	}
}

// retains requires view to retain exactly the sources and the resolutions
// named, in any order of sources.
func retains(t *testing.T, view lifecycle.View, sources []prerequisites.DependencySource, resolutions ...prerequisites.Definition) {
	t.Helper()
	held := view.Controller().State
	byID := func(x, y prerequisites.DependencySource) int { return strings.Compare(x.ID, y.ID) }
	if got, want := slices.SortedFunc(slices.Values(held.RetainedSources), byID), slices.SortedFunc(slices.Values(sources), byID); !slices.Equal(got, want) {
		t.Fatalf("retained sources = %+v, want %+v", got, want)
	}
	if !slices.EqualFunc(held.RetainedDefinitions, resolutions, prerequisites.SameDefinition) {
		t.Fatalf("retained %d resolutions, want %d", len(held.RetainedDefinitions), len(resolutions))
	}
}

// syntheticResolution is one complete native resolution, as a controller stage
// freezes before it installs anything.
func syntheticResolution(t *testing.T) prerequisites.Definition {
	t.Helper()
	platform := prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}
	source := func(id, url string) prerequisites.DependencySource {
		return prerequisites.DependencySource{ID: id, URL: url, SHA256: strings.Repeat("a", 64), Bytes: 64}
	}
	bootstrap, err := prerequisites.CanonicalBootstrap(prerequisites.BootstrapDefinition{
		Format: "bootwright.controller.bootstrap-v1", Platform: platform, PythonIntent: "latest", AnsibleIntent: "latest",
		PythonVersion: "3.14.7", AnsibleVersion: "2.21.4", PythonExecutable: "python/bin/python3.14", SitePackages: "python/lib/python3.14/site-packages/",
		Sources: []prerequisites.DependencySource{
			source("python", "https://github.com/astral-sh/python-build-standalone/releases/download/20260901/cpython-3.14.7%2B20260901-x86_64-unknown-linux-gnu-install_only_stripped.tar.gz"),
			source("ansible", "https://files.pythonhosted.org/packages/ansible_core-2.21.4-py3-none-any.whl"),
			source("urllib3", "https://files.pythonhosted.org/packages/urllib3-2.7.0-py3-none-any.whl"),
		},
		Wheels: []prerequisites.BootstrapWheel{{Name: "ansible-core", Version: "2.21.4", SourceID: "ansible"}, {Name: "urllib3", Version: "2.7.0", SourceID: "urllib3"}},
		Metadata: []prerequisites.DependencySource{
			source("python-metadata", "https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json"),
			source("ansible-metadata", "https://pypi.org/pypi/ansible-core/json"),
		},
		ProjectionSHA256: strings.Repeat("b", 64), FileCount: 10, ExpandedBytes: 100, AutomationDigest: strings.Repeat("c", 64),
		Execution:         prerequisites.ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Files: []prerequisites.InstalledFile{}, Links: []prerequisites.InstalledLink{}, Preload: []string{}},
		ExecutionPackages: []string{"glibc", "libgcc"},
	})
	if err != nil {
		t.Fatal(err)
	}
	native := prerequisites.NativeResolvedPlan{
		Format: "bootwright.native-plan-v1", Platform: platform, Solver: "dnf5", SolverVersion: "5.2.0", Requests: controller.DefaultDependencyVersions(),
		Requirements: prerequisites.NativeRequirements{ContainerRuntime: true},
		Repositories: []prerequisites.NativeRepository{{ID: "base", BaseURL: "https://packages.example.test/fedora", MetadataSHA256: strings.Repeat("d", 64)}},
		Roots:        []prerequisites.NativeRoot{}, Packages: []prerequisites.NativePackage{}, Actions: []prerequisites.NativeAction{},
		BeforeSHA256: strings.Repeat("e", 64), AfterSHA256: strings.Repeat("e", 64),
	}
	for _, root := range []struct{ key, name string }{{"podman", "podman"}, {"openssh", "openssh-clients"}, {"nmstate", "nmstate"}} {
		identity := prerequisites.NativeIdentity{Name: root.name, Version: "1.2.3", Release: "1.fc43", Architecture: "x86_64"}
		native.Roots = append(native.Roots, prerequisites.NativeRoot{Key: root.key, Requested: "latest", Package: identity})
		native.Packages = append(native.Packages, prerequisites.NativePackage{
			Name: identity.Name, Version: identity.Version, Release: identity.Release, Architecture: identity.Architecture,
			Signer: strings.Repeat("f", 40), Source: source(root.name, "https://packages.example.test/fedora/"+root.name+".rpm"),
		})
	}
	if native, err = prerequisites.CanonicalNativePlan(native); err != nil {
		t.Fatal(err)
	}
	definition, err := prerequisites.NewResolvedDefinition(bootstrap, native)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func succeeds(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s failed: %v", what, err)
	}
}

func refuses(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s succeeded", what)
	}
}

func holds(t *testing.T, area operationstore.Area, target, want string) {
	t.Helper()
	data, found, err := area.Read(context.Background(), target, bound)
	if err != nil || !found || string(data) != want {
		t.Fatalf("read of %s = %q %t (%v), want %q", target, data, found, err, want)
	}
}

func lists(t *testing.T, area operationstore.Area) {
	t.Helper()
	entries, err := area.Entries(context.Background(), "")
	if err != nil || len(entries) != 0 {
		t.Fatalf("entries = %+v (%v), want none", entries, err)
	}
}
