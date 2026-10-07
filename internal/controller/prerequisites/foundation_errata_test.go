package prerequisites

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
)

// qualifyingFoundation is the fixture's foundation inspector on a host whose
// builds the inspector qualified from the RPM database, or none.
type qualifyingFoundation struct {
	*testFoundation
	qualified *QualifiedFoundation
}

func (f qualifyingFoundation) Inspect(ctx context.Context, platform Platform) (FoundationInspection, error) {
	inspection, err := f.testFoundation.Inspect(ctx, platform)
	inspection.Qualified = CloneQualifiedFoundation(f.qualified)
	return inspection, err
}

// countingRuntime fails every native effect it is asked for, so a setup that
// must not install anything proves it asked for nothing.
type countingRuntime struct{ calls int }

func (r *countingRuntime) Prepare(context.Context, BundleArea, Platform, Definition, SetupEgress, func(context.Context, NativePreparation) error, func(ProgressEvent), RunOutput) (ActionResult, error) {
	r.calls++
	return ActionResult{}, errors.New("no native effect may run")
}

func (r *countingRuntime) Recover(context.Context, BundleArea, Platform, Definition, SetupEgress, NativePreparation, func(ProgressEvent), RunOutput) (ActionResult, error) {
	r.calls++
	return ActionResult{}, errors.New("no native effect may run")
}

// errataCompiled is the compiled requirement the errata host's builds are
// qualified from, and errataQualified the foundation they prove.
var errataCompiled = ExecutionRequirement{Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/var/lib/rpm/.rpm.lock",
	Files: []InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: hex.EncodeToString(make([]byte, 32))}, {Path: "/usr/lib64/libgcc_s-11-20240719.so.1", SHA256: hex.EncodeToString(make([]byte, 32))}}, Links: []InstalledLink{}, Preload: []string{}}

func errataQualified() *QualifiedFoundation {
	return &QualifiedFoundation{
		Execution: ExecutionRequirement{Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/var/lib/rpm/.rpm.lock", Files: []InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: hex.EncodeToString(make([]byte, 32))}, {Path: "/usr/lib64/libgcc_s-11-20250101.so.1", SHA256: hex.EncodeToString(slices.Repeat([]byte{1}, 32))}}, Links: []InstalledLink{}, Preload: []string{}},
		Packages:  []FoundationBuild{{Name: "glibc", Build: "2.34-276.el9_8", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2"}}, {Name: "libgcc", Build: "11.5.0-15.el9", Files: []string{"/usr/lib64/libgcc_s-11-20250101.so.1"}}},
	}
}

// launchingBundle is the fixture's bundle manager, keeping the requirement
// each inspection's launches would verify for errataCompiled.
type launchingBundle struct {
	*testBundle
	launches []ExecutionRequirement
}

func (b *launchingBundle) Inspect(ctx context.Context, area BundleArea, definition Definition, execute bool) (BundleInspection, error) {
	b.launches = append(b.launches, LaunchRequirementFor(ctx, errataCompiled))
	return b.testBundle.Inspect(ctx, area, definition, execute)
}

const errataSummary = "glibc 2.34-276.el9_8, libgcc 11.5.0-15.el9 (vendor-signed, qualified within rhel 9.8)"

// errataFixture is a set-up host whose foundation inspector then qualifies,
// with a runtime that refuses every effect.
func errataFixture(t *testing.T) (*fixture, *qualifyingFoundation, *countingRuntime, *launchingBundle) {
	t.Helper()
	f := newFixture(t)
	if report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil || report.Outcome != "changed" || f.store.state.Receipt.Foundation != nil {
		t.Fatalf("the first setup: %#v %v", report, err)
	}
	foundation := &qualifyingFoundation{testFoundation: &f.foundation}
	runtime := &countingRuntime{}
	options := f.service.options
	options.Foundation = foundation
	bundle := &launchingBundle{testBundle: &f.bundle}
	f.service = New(&f.store, &f.compiler, &f.host, &f.catalog, bundle, runtime, options)
	return f, foundation, runtime, bundle
}

// A z-stream errata of glibc or libgcc needs one setup: it reports the
// foundation ready as the qualified builds, plans to record them, and
// publishes a new complete receipt carrying them with both actions verified
// and nothing prepared, installed or reacquired; the next setup is unchanged.
func TestAZStreamErrataNeedsOneSetup(t *testing.T) {
	f, foundation, runtime, bundle := errataFixture(t)
	foundation.qualified = errataQualified()
	before, prepares := f.store.state.Receipt, f.bundle.prepares
	report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("setup over the errata host: %#v %v", report, err)
	}
	ready := Check{ID: "execution-foundation", Required: testFoundationBuilds, Observed: errataSummary, Status: "ready", Scope: HostScope}
	if check, _ := settledCheck(&f.plan, "execution-foundation"); check != ready {
		t.Fatalf("the plan reports %+v", check)
	}
	if check, _ := settledCheck(report, "execution-foundation"); check != ready {
		t.Fatalf("the result reports %+v", check)
	}
	if !slices.Contains(f.plan.Actions, "Re-qualify the execution foundation: "+errataSummary) {
		t.Fatalf("the plan presents %q", f.plan.Actions)
	}
	receipt := f.store.state.Receipt
	if receipt.ID == before.ID || receipt.Status != "complete" || !reflect.DeepEqual(receipt.Foundation, errataQualified()) || receipt.PlanDigest == before.PlanDigest || receipt.CatalogDigest != before.CatalogDigest {
		t.Fatalf("the published receipt is %+v after %+v", receipt, before)
	}
	for _, action := range receipt.Actions {
		var request map[string]any
		if json.Unmarshal(action.Request, &request) != nil || request["readyBefore"] != true || action.Outcome != "unchanged" {
			t.Fatalf("the action %s ran: %s %s", action.ID, action.Request, action.Outcome)
		}
	}
	launch := errataQualified().Execution
	if len(bundle.launches) == 0 || !reflect.DeepEqual(bundle.launches[0], launch) {
		t.Fatalf("the bundle inspection launches %+v, want the qualified %+v", bundle.launches, launch)
	}
	if f.bundle.prepares != prepares || runtime.calls != 0 {
		t.Fatalf("the errata setup prepared %d bundles and %d native effects", f.bundle.prepares-prepares, runtime.calls)
	}
	writes := f.store.writes
	report, err = f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
	if err != nil || report.Outcome != "unchanged" || f.store.writes != writes {
		t.Fatalf("the next setup: %#v %v", report, err)
	}
	if check, _ := settledCheck(report, "execution-foundation"); check != ready {
		t.Fatalf("the next setup reports %+v", check)
	}
	if report, err := f.service.Check(context.Background(), CheckRequest{}); err != nil || report.Outcome != "ready" {
		t.Fatalf("preflight after the errata setup: %#v %v", report, err)
	}
}

// Preflight over a receipt that records another foundation than the one the
// host's builds qualify is not ready, names that check and offers setup,
// whichever side moved.
func TestPreflightNamesSetupForAnUnrecordedQualification(t *testing.T) {
	f, foundation, _, _ := errataFixture(t)
	foundation.qualified = errataQualified()
	report, err := f.service.Check(context.Background(), CheckRequest{})
	want := Check{ID: "execution-foundation", Required: testFoundationBuilds, Observed: errataSummary + ", which setup has not recorded", Status: "not-ready", Scope: HostScope}
	if code(err) != "preflight.failed" || report.Outcome != "not-ready" || report.Next != setupInvocation {
		t.Fatalf("preflight over an unrecorded qualification: %#v %v", report, err)
	}
	if check, _ := settledCheck(report, "execution-foundation"); check != want {
		t.Fatalf("preflight reports %+v", check)
	}
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	foundation.qualified = nil
	report, err = f.service.Check(context.Background(), CheckRequest{})
	want.Observed = testFoundationBuilds + ", while setup recorded " + errataSummary
	if code(err) != "preflight.failed" || report.Next != setupInvocation {
		t.Fatalf("preflight over a host back on the compiled builds: %#v %v", report, err)
	}
	if check, _ := settledCheck(report, "execution-foundation"); check != want {
		t.Fatalf("preflight reports %+v", check)
	}
}

// A host holding the compiled builds records no foundation: the receipt's
// bytes and plan digest are exactly the ones a build without qualification
// published.
func TestAnUnchangedHostRecordsNoFoundation(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(f.store.state.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != "d9f6242588aa274d9811258b8c3d3ee13f08f354e8b0d4e79c48ef4246837ca1" || f.store.state.Receipt.PlanDigest != "7c5a5ad39ab65edd58cc7cad4e80bf06006015946a506c4c73b87966fb5cd286" {
		t.Fatalf("an unchanged host published %s with plan digest %s", data, f.store.state.Receipt.PlanDigest)
	}
}

// A recorded foundation is admitted only in the shape of the compiled
// requirement it was qualified from, and a launch verifies it under the
// definition's interpreter; one in any other shape launches the definition's
// own requirement, which the guard then verifies.
func TestARecordedFoundationKeepsTheCompiledShape(t *testing.T) {
	digest := func(b byte) string { return hex.EncodeToString(slices.Repeat([]byte{b}, 32)) }
	compiled := ExecutionRequirement{PythonExecutable: "python/bin/python3.14", Loader: "/usr/lib64/ld-linux-x86-64.so.2", LockPath: "/var/lib/rpm/.rpm.lock",
		Files:   []InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: digest(1)}, {Path: "/usr/lib64/libc.so.6", SHA256: digest(2)}, {Path: "/usr/lib64/libgcc_s-11-20240719.so.1", SHA256: digest(3)}},
		Links:   []InstalledLink{{Path: "/lib64", Target: "usr/lib64"}, {Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-11-20240719.so.1"}},
		Preload: []string{"/usr/lib64/libc.so.6", "/usr/lib64/libgcc_s-11-20240719.so.1"}}
	valid := func() QualifiedFoundation {
		return QualifiedFoundation{
			Execution: ExecutionRequirement{Loader: compiled.Loader, LockPath: compiled.LockPath,
				Files:   []InstalledFile{{Path: "/usr/lib64/ld-linux-x86-64.so.2", SHA256: digest(1)}, {Path: "/usr/lib64/libc.so.6", SHA256: digest(4)}, {Path: "/usr/lib64/libgcc_s-11-20250101.so.1", SHA256: digest(5)}},
				Links:   []InstalledLink{{Path: "/lib64", Target: "usr/lib64"}, {Path: "/usr/lib64/libgcc_s.so.1", Target: "libgcc_s-11-20250101.so.1"}},
				Preload: []string{"/usr/lib64/libc.so.6", "/usr/lib64/libgcc_s-11-20250101.so.1"}},
			Packages: []FoundationBuild{{Name: "glibc", Build: "2.34-276.el9_8", Files: []string{"/usr/lib64/ld-linux-x86-64.so.2", "/usr/lib64/libc.so.6"}}, {Name: "libgcc", Build: "11.5.0-15.el9", Files: []string{"/usr/lib64/libgcc_s-11-20250101.so.1"}}},
		}
	}
	foundation := valid()
	if err := ValidateQualifiedFoundation(foundation, compiled); err != nil {
		t.Fatalf("a qualified foundation was refused: %v", err)
	}
	definition := Definition{Execution: compiled}
	launch := LaunchRequirement(SetupReceipt{Definition: &definition, Foundation: &foundation})
	if launch.PythonExecutable != compiled.PythonExecutable || !slices.Equal(launch.Files, foundation.Execution.Files) || !slices.Equal(launch.Links, foundation.Execution.Links) {
		t.Fatalf("the launch verifies %+v", launch)
	}
	if got := LaunchRequirementFor(WithLaunchFoundation(context.Background(), &foundation), compiled); !reflect.DeepEqual(got, launch) {
		t.Fatalf("setup's launch verifies %+v, not %+v", got, launch)
	}
	if got := LaunchRequirement(SetupReceipt{Definition: &definition}); !reflect.DeepEqual(got, compiled) {
		t.Fatalf("a receipt without a foundation launches %+v", got)
	}
	if got := LaunchRequirementFor(context.Background(), compiled); !reflect.DeepEqual(got, compiled) {
		t.Fatalf("setup without a qualification launches %+v", got)
	}
	for name, change := range map[string]func(*QualifiedFoundation){
		"an interpreter":      func(f *QualifiedFoundation) { f.Execution.PythonExecutable = "python/bin/python3.14" },
		"another loader":      func(f *QualifiedFoundation) { f.Execution.Loader = "/usr/lib64/ld-2.so" },
		"another lock":        func(f *QualifiedFoundation) { f.Execution.LockPath = "/usr/lib/sysimage/rpm/.rpm.lock" },
		"another path":        func(f *QualifiedFoundation) { f.Execution.Files[1].Path = "/usr/lib64/libm.so.6" },
		"an uppercase digest": func(f *QualifiedFoundation) { f.Execution.Files[1].SHA256 = "AA" + f.Execution.Files[1].SHA256[2:] },
		"a stale link":        func(f *QualifiedFoundation) { f.Execution.Links[1].Target = "libgcc_s-11-20240719.so.1" },
		"a stale preload":     func(f *QualifiedFoundation) { f.Execution.Preload[1] = "/usr/lib64/libgcc_s-11-20240719.so.1" },
		"an unattributed file": func(f *QualifiedFoundation) {
			f.Packages = f.Packages[:1]
		},
		"a build with a space": func(f *QualifiedFoundation) { f.Packages[0].Build = "2.34 276" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := valid()
			change(&changed)
			if err := ValidateQualifiedFoundation(changed, compiled); err == nil {
				t.Fatal("an out-of-shape foundation was admitted")
			}
			if got := LaunchRequirement(SetupReceipt{Definition: &definition, Foundation: &changed}); !reflect.DeepEqual(got, compiled) {
				t.Fatalf("an out-of-shape foundation launches %+v", got)
			}
		})
	}
}
