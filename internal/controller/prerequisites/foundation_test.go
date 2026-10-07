package prerequisites

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/availability"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// settledCheck is the check a report settled under id, and its position.
func settledCheck(report *Report, id string) (Check, int) {
	index := slices.IndexFunc(report.Checks, func(check Check) bool { return check.ID == id })
	if index < 0 {
		return Check{}, index
	}
	return report.Checks[index], index
}

// The execution foundation check settles right after the installed host.
// Setup's plan and preflight's report name the package builds the host holds;
// a drifted foundation settles not-ready, observed at the path that differs,
// and its named refusal stops setup before any plan and preflight alike, with
// no next command, because only restoring the host settles it; a dry run
// reports the check unverified without inspecting the host, and a composition
// without the inspector offers neither command.
func TestSetupAndPreflightReportTheExecutionFoundation(t *testing.T) {
	ready := Check{ID: "execution-foundation", Required: testFoundationBuilds, Observed: testFoundationBuilds, Status: "ready", Scope: HostScope}
	inOrder := func(t *testing.T, report *Report) {
		t.Helper()
		check, position := settledCheck(report, "execution-foundation")
		_, host := settledCheck(report, "installed-host")
		_, bundle := settledCheck(report, "execution-bundle")
		if position != host+1 || bundle <= position {
			t.Fatalf("the foundation check %+v sits at %d between %d and %d: %+v", check, position, host, bundle, report.Checks)
		}
	}
	t.Run("ready", func(t *testing.T) {
		f := newFixture(t)
		report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		if err != nil || report.Outcome != "changed" {
			t.Fatalf("setup: %#v %v", report, err)
		}
		if check, _ := settledCheck(&f.plan, "execution-foundation"); check != ready {
			t.Fatalf("setup's plan reports %+v", check)
		}
		inOrder(t, &f.plan)
		report, err = f.service.Check(context.Background(), CheckRequest{})
		if err != nil || report.Outcome != "ready" {
			t.Fatalf("preflight: %#v %v", report, err)
		}
		if check, _ := settledCheck(report, "execution-foundation"); check != ready {
			t.Fatalf("preflight reports %+v", check)
		}
		inOrder(t, report)
	})
	t.Run("drifted", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		drifted := diagnostics.NewFailureWithRemediation("controller.unsupported", "the provided execution foundation differs at /usr/lib64/libc.so.6, from glibc 2.34-275.el9_8, which holds other content than this build pins", "", "Install exactly glibc 2.34-275.el9_8 again with dnf, hold it with dnf versionlock, then repeat this command.")
		f.foundation.drift, f.foundation.refusal = "/usr/lib64/libc.so.6", drifted
		f.events = nil
		writes, prepares := f.store.writes, f.bundle.prepares
		want := Check{ID: "execution-foundation", Required: testFoundationBuilds, Observed: "differs at /usr/lib64/libc.so.6", Status: "not-ready", Scope: HostScope}
		report, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		if !errors.Is(err, drifted) || report == nil || report.Next != "" || report.PlanPresented {
			t.Fatalf("setup over a drifted foundation: %#v %v", report, err)
		}
		if check, _ := settledCheck(report, "execution-foundation"); check != want {
			t.Fatalf("setup reports %+v", check)
		}
		report, err = f.service.Check(context.Background(), CheckRequest{})
		if !errors.Is(err, drifted) || report == nil || report.Outcome != "not-ready" || report.Next != "" {
			t.Fatalf("preflight over a drifted foundation: %#v %v", report, err)
		}
		if check, _ := settledCheck(report, "execution-foundation"); check != want {
			t.Fatalf("preflight reports %+v", check)
		}
		if slices.Contains(f.events, "present") || f.store.writes != writes || f.bundle.prepares != prepares {
			t.Fatalf("a drifted foundation was acted on: events=%v writes=%d prepares=%d", f.events, f.store.writes-writes, f.bundle.prepares-prepares)
		}
	})
	t.Run("dry run", func(t *testing.T) {
		f := newFixture(t)
		report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
		if err != nil {
			t.Fatal(err)
		}
		if check, _ := settledCheck(report, "execution-foundation"); check.Status != "unverified" || check.Observed != "unverified" || f.foundation.inspections != 0 {
			t.Fatalf("the dry run reports %+v after %d inspections", check, f.foundation.inspections)
		}
	})
	t.Run("no inspector", func(t *testing.T) {
		f := newFixture(t)
		options := f.service.options
		options.Foundation = nil
		service := New(&f.store, &f.compiler, &f.host, &f.catalog, &f.bundle, nil, options)
		if report, err := service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); !errors.Is(err, availability.ErrNotImplemented) || report != nil {
			t.Fatalf("setup without the inspector: %#v %v", report, err)
		}
		if report, err := service.Check(context.Background(), CheckRequest{}); !errors.Is(err, availability.ErrNotImplemented) || report != nil || f.store.reads != 0 {
			t.Fatalf("preflight without the inspector: %#v %v", report, err)
		}
	})
}

// fipsHost is the installed host with its kernel's FIPS mode.
type fipsHost struct {
	*testHost
	enabled bool
}

func (h fipsHost) FIPSMode(context.Context) (bool, error) { return h.enabled, nil }

// Preflight reports the host's FIPS mode beside the execution foundation: on a
// FIPS-mode host with the statement that Bootwright's runtime brings its own
// cryptography, outside the host's FIPS-validated modules (D108). The check is
// informational: it is always ready, its requirement is what it observed, and
// readiness, the plan and the next command are those the same host reports
// without it.
func TestPreflightReportsTheHostsFIPSModeWithTheStatement(t *testing.T) {
	for mode, enabled := range map[string]bool{fipsEnabled: true, "disabled": false} {
		t.Run(mode, func(t *testing.T) {
			want := Check{ID: "fips-mode", Required: mode, Observed: mode, Status: "ready", Scope: HostScope}
			plain, fips := newFixture(t), newFixture(t)
			fips.service = New(&fips.store, &fips.compiler, fipsHost{testHost: &fips.host, enabled: enabled}, &fips.catalog, &fips.bundle, nil, fips.service.options)
			before, _ := plain.service.Check(context.Background(), CheckRequest{})
			report, err := fips.service.Check(context.Background(), CheckRequest{})
			if code(err) != "preflight.failed" || report.Outcome != before.Outcome || report.Next != before.Next {
				t.Fatalf("preflight of a host not yet set up: %#v %v, want the outcome and next command of %#v", report, err, before)
			}
			if check, position := settledCheck(report, "fips-mode"); check != want || report.Checks[position-1].ID != "execution-foundation" {
				t.Fatalf("preflight reports %+v in %+v", check, report.Checks)
			}
			for _, f := range []*fixture{plain, fips} {
				if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(fips.plan.Actions, plain.plan.Actions) {
				t.Fatalf("the FIPS mode changed the plan: %q, want %q", fips.plan.Actions, plain.plan.Actions)
			}
			report, err = fips.service.Check(context.Background(), CheckRequest{})
			if err != nil || report.Outcome != "ready" || report.Next != "" {
				t.Fatalf("preflight of a ready host: %#v %v", report, err)
			}
			if check, _ := settledCheck(report, "fips-mode"); check != want {
				t.Fatalf("preflight of a ready host reports %+v", check)
			}
		})
	}
}
