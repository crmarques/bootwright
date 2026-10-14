package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// retentionFixture is a host whose setup replaces runtime A with this build's
// runtime, while B and C are older runtimes still published.
func retentionFixture(t *testing.T) (*setupFixture, map[string]string) {
	t.Helper()
	f := newSetupFixture(t)
	f.prepared()
	f.python.ready = false
	current := f.python.runtime().Digest
	digests := map[string]string{"current": current, "A": strings.Repeat("a", 63) + "0", "B": strings.Repeat("b", 63) + "0", "C": strings.Repeat("c", 63) + "0"}
	f.store.view.State.Runtimes = SetupRuntimes{Current: digests["A"], Previous: digests["B"]}
	f.store.published = []string{digests["A"], digests["B"], digests["C"], current}
	f.python.named = map[string][]string{
		current:      {strings.Repeat("a", 64), strings.Repeat("b", 64)},
		digests["A"]: {strings.Repeat("c", 64)},
		digests["B"]: {strings.Repeat("d", 64)},
		digests["C"]: {strings.Repeat("e", 64)},
	}
	return f, digests
}

// Setup keeps its own runtime and the one it replaced, retires the rest and
// prunes the wheel cache to what the kept runtimes name plus the installer.
func TestRetentionKeepsTheCurrentAndThePreviousRuntime(t *testing.T) {
	f, digests := retentionFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if runtimes := f.store.view.State.Runtimes; runtimes.Current != digests["current"] || runtimes.Previous != digests["A"] {
		t.Fatalf("the record keeps %+v, want the current runtime and A", runtimes)
	}
	if want := []string{digests["A"], digests["current"]}; !slices.Equal(f.store.published, want) {
		t.Fatalf("retention left %q, want %q", f.store.published, want)
	}
	if receipt := slices.Index(f.events, "record"); receipt < 0 || slices.Index(f.events, "retire "+digests["B"][:4]) < slices.Index(f.events, "build") {
		t.Fatalf("retention did not follow the build and the receipt: %v", f.events)
	}
	if want := []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)}; !slices.Equal(f.store.kept, want) {
		t.Fatalf("the wheel cache keeps %q, want %q", f.store.kept, want)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("retention with nothing in use warned %+v", report.Warnings)
	}
}

// A runtime a launch holds is skipped, reported, and its wheels stay.
func TestRetentionSkipsARuntimeInUse(t *testing.T) {
	f, digests := retentionFixture(t)
	f.store.held = map[string]bool{digests["C"]: true}
	report, err := f.service.Setup(context.Background(), SetupRequest{})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if !slices.Contains(f.store.published, digests["C"]) || slices.Contains(f.store.published, digests["B"]) {
		t.Fatalf("retention left %q", f.store.published)
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Code != retentionCode || report.Warnings[0].Message != "kept: 3 runtimes (1 in use)" {
		t.Fatalf("the held runtime was reported as %+v", report.Warnings)
	}
	if !slices.Contains(f.store.kept, strings.Repeat("e", 64)) || slices.Contains(f.store.kept, strings.Repeat("d", 64)) {
		t.Fatalf("the wheel cache keeps %q", f.store.kept)
	}
}

// A ready host writes nothing, so it retires nothing either.
func TestAReadySetupRetiresNothing(t *testing.T) {
	f, _ := retentionFixture(t)
	f.python.ready = true
	f.store.view.State.Runtimes = SetupRuntimes{Current: f.python.runtime().Digest}
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	if len(f.store.published) != 4 || f.store.kept != nil {
		t.Fatalf("a ready setup retired or pruned: %q %q", f.store.published, f.store.kept)
	}
}

func rebuildFixture(t *testing.T) (*setupFixture, RuntimeRebuilder) {
	t.Helper()
	f := newSetupFixture(t)
	f.prepared()
	f.python.ready, f.python.cached = false, true
	return f, NewRuntimeRebuilder(f.store, f.python, testLock)
}

// The rebuild reaches no network, asks nothing and writes no setup record: it
// waits for the setup lock and republishes from the cache alone.
func TestARebuildUsesOnlyTheWheelCache(t *testing.T) {
	f, rebuilder := rebuildFixture(t)
	if err := rebuilder.Rebuild(context.Background(), f.python.runtime()); err != nil {
		t.Fatalf("the rebuild failed: %v", err)
	}
	if !slices.Equal(f.effects(), []string{"build", "publish"}) || f.count("await") != 1 || f.count("confirm") != 0 {
		t.Fatalf("the rebuild ran %v", f.events)
	}
}

func TestARebuildBuildsNothingWithoutEveryWheelCached(t *testing.T) {
	f, rebuilder := rebuildFixture(t)
	f.python.cached = false
	err := rebuilder.Rebuild(context.Background(), f.python.runtime())
	if !errors.Is(err, ErrRuntimeWheelMissing) || !errors.Is(err, ErrRuntimeUnavailable) || len(f.effects()) != 0 {
		t.Fatalf("an uncached wheel rebuilt: %v %v", err, f.events)
	}
}

func TestARebuildIsRefusedWhereOnlySetupCanPrepareTheRuntime(t *testing.T) {
	for name, test := range map[string]func(*setupFixture, *Runtime){
		"no receipt":           func(f *setupFixture, _ *Runtime) { f.store.view = SetupView{} },
		"interpreter gone":     func(f *setupFixture, _ *Runtime) { f.python.installed = false },
		"another interpreter":  func(_ *setupFixture, runtime *Runtime) { runtime.Interpreter.Path = "/usr/bin/python3.13" },
		"another lock":         func(_ *setupFixture, runtime *Runtime) { runtime.LockDigest = strings.Repeat("9", 64) },
		"an interrupted setup": func(f *setupFixture, _ *Runtime) { f.store.view.State.Pending = &PendingSetup{ID: "setup-000042"} },
		"another build's record": func(f *setupFixture, _ *Runtime) {
			f.store.view = SetupView{Exists: true, Foreign: "bootwright.setup-state-v9"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, rebuilder := rebuildFixture(t)
			runtime := f.python.runtime()
			test(f, &runtime)
			err := rebuilder.Rebuild(context.Background(), runtime)
			if err == nil || !errors.Is(err, ErrRuntimeUnavailable) && codeOf(err) != "controller.state" || f.count("build") != 0 {
				t.Fatalf("the rebuild returned %v after %v", err, f.events)
			}
		})
	}
}

// The adapter's setup remedy does not fit a launch: a build that fails with
// every wheel cached reaches the launch as a rebuild failure naming its cause.
func TestAFailedBuildIsARebuildFailure(t *testing.T) {
	f, rebuilder := rebuildFixture(t)
	f.python.buildErr = &ScopedFailure{Message: "pip could not install the locked wheels", Correction: "restore the wheels"}
	err := rebuilder.Rebuild(context.Background(), f.python.runtime())
	reported := diagnostics.Of(err)
	if !errors.Is(err, ErrRuntimeRebuildFailed) || len(reported) != 1 || reported[0].Message != "pip could not install the locked wheels" || errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("a failed build returned %v", err)
	}
}

// A runtime another launch rebuilt while this one waited is adopted as is.
func TestARebuildAdoptsARuntimePublishedMeanwhile(t *testing.T) {
	f, rebuilder := rebuildFixture(t)
	f.python.ready = true
	if err := rebuilder.Rebuild(context.Background(), f.python.runtime()); err != nil || f.count("build") != 0 {
		t.Fatalf("the rebuild returned %v after %v", err, f.events)
	}
}
