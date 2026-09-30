// Package workspacecontract is the shared contract suite for
// lifecycle.Workspace. Every implementation's tests run it, the in-memory
// doubles included, so a double cannot accept what the Workspace adapter
// refuses. No production package imports it.
package workspacecontract

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// Subject is one fresh workspace holding the ready context Context, with
// empty operation and run areas, on a host whose completed controller setup
// identifies it as Host and binds no context.
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
	read(t, s, func(view lifecycle.View) { escaped["an inspection's operation area"] = view.Operations() })
	run(t, s, func(view lifecycle.RunView) { escaped["a bounded run's run area"] = view.Runs() })
	mutate(t, s, func(tx lifecycle.Transaction) { escaped["a transaction's operation area"] = tx.Operations() })
	for name, area := range escaped {
		if _, _, err := area.Read(ctx, "r.json", bound); err == nil {
			t.Fatalf("%s read after its callback returned", name)
		}
		if err := area.WriteExclusive(ctx, "r.json", []byte("late\n")); err == nil {
			t.Fatalf("%s wrote after its callback returned", name)
		}
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
