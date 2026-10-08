package installation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

// runDeadline is the deadline one operation's run states for a frozen request.
func runDeadline(t *testing.T, request Request, operation string) time.Duration {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	call := lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: request.Identity.Block, Request: canonical},
			RequestDigest:   "digest",
		},
		Material: fleetMaterial(),
	}
	// The run itself is not under test: only the request it was handed.
	runner := &fakeRunner{err: errors.New("stopped")}
	capability := New(runner)
	switch operation {
	case "apply":
		_, _ = capability.Apply(context.Background(), call)
	case "destroy":
		_, _ = capability.Destroy(context.Background(), call)
	case "observe":
		_, _ = capability.Observe(context.Background(), call)
	case "observe-removal":
		_, _ = capability.ObserveRemoval(context.Background(), call)
	default:
		t.Fatalf("no run measures the %s operation", operation)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("%s ran the adapter %d times", operation, len(runner.requests))
	}
	return runner.requests[0].Deadline
}

// Every run of an installation is bounded by a deadline derived from the
// budgets its frozen request carries. It covers every pause those budgets
// allow with room for the media work around them, so the runner never kills
// an installation still inside one of its waits, and it stays within the
// ceiling the runner holds every request to, so the runner never cuts it
// short. A fixed two-hour deadline was shorter than the waits alone.
func TestEveryCapabilityDeadlineCoversItsFrozenBudgets(t *testing.T) {
	checksummed := api.NewObject(api.MachineImage, "rhel-9-8-boot", api.Value{}, api.MapValue(
		text("bootMedia", "local-media:rhel-9.8-x86_64-boot.iso"),
		text("checksum", "SHA256:"+strings.ToUpper(strings.Repeat("0123456789abcdef", 4))),
	))
	sshPlaced := artifactServer(text("machineRef", "services"), text("bindAddress", "192.0.2.2"))
	for name, catalog := range map[string]api.Catalog{
		"lab-rhel": labCatalog(), "lab-rhel-checksummed": labCatalog(checksummed),
		"lab-rhel-ssh-placed": labCatalog(servicesHost(), sshPlaced),
	} {
		request, _ := onlyRequest(t, catalog)
		var waits time.Duration
		for wait, budget := range map[string]Budget{
			"installer": request.Budgets.Installer, "identity": request.Budgets.Identity, "reachability": request.Budgets.Reachability,
		} {
			if budget.Attempts <= 0 || budget.DelaySeconds <= 0 {
				t.Fatalf("%s freezes the %s wait as %+v, which waits for nothing", name, wait, budget)
			}
			waits += time.Duration(budget.Attempts*budget.DelaySeconds) * time.Second
		}
		for _, operation := range []string{"apply", "observe", "observe-removal", "destroy"} {
			deadline := runDeadline(t, request, operation)
			if deadline <= waits {
				t.Errorf("%s %s runs under %s, which does not cover the %s its budgets wait", name, operation, deadline, waits)
			}
			if deadline > lifecycle.MaxDeadline {
				t.Errorf("%s %s states %s, past the %s ceiling the runner holds it to", name, operation, deadline, lifecycle.MaxDeadline)
			}
		}
	}
}

// pauses is every pause a request's budgets allow, counted here rather than
// through the derivation under test.
func pauses(budgets Budgets) time.Duration {
	seconds := budgets.Installer.Attempts*budgets.Installer.DelaySeconds +
		budgets.Identity.Attempts*budgets.Identity.DelaySeconds +
		budgets.Reachability.Attempts*budgets.Reachability.DelaySeconds
	return time.Duration(seconds) * time.Second
}

// The deadline follows every budget the request froze, not this build's own,
// so an operation frozen with other budgets is bounded by exactly the waits it
// froze. The budgets differ from this build's and from each other, and each
// pauses longer than the margin, so a term the derivation dropped, or took
// from this build instead, cannot hide inside the margin; and each field moved
// alone must move the deadline by exactly what it adds.
func TestTheDeadlineFollowsTheBudgetsTheRequestFroze(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	request.Budgets = Budgets{
		Installer:    Budget{Attempts: 211, DelaySeconds: 37},
		Identity:     Budget{Attempts: 137, DelaySeconds: 59},
		Reachability: Budget{Attempts: 401, DelaySeconds: 19},
	}
	for wait, budget := range map[string]Budget{
		"installer": request.Budgets.Installer, "identity": request.Budgets.Identity, "reachability": request.Budgets.Reachability,
	} {
		if pause := time.Duration(budget.Attempts*budget.DelaySeconds) * time.Second; pause <= mediaMargin {
			t.Fatalf("the %s wait pauses %s, no longer than the %s margin it could hide in", wait, pause, mediaMargin)
		}
	}
	fields := map[string]func(*Budgets) *int{
		"nothing":                   nil,
		"installer.attempts":        func(b *Budgets) *int { return &b.Installer.Attempts },
		"installer.delaySeconds":    func(b *Budgets) *int { return &b.Installer.DelaySeconds },
		"identity.attempts":         func(b *Budgets) *int { return &b.Identity.Attempts },
		"identity.delaySeconds":     func(b *Budgets) *int { return &b.Identity.DelaySeconds },
		"reachability.attempts":     func(b *Budgets) *int { return &b.Reachability.Attempts },
		"reachability.delaySeconds": func(b *Budgets) *int { return &b.Reachability.DelaySeconds },
	}
	for name, field := range fields {
		frozen := request
		if field != nil {
			*field(&frozen.Budgets) += 7
		}
		want := pauses(frozen.Budgets) + mediaMargin
		for _, operation := range []string{"apply", "observe", "observe-removal", "destroy"} {
			if got := runDeadline(t, frozen, operation); got != want {
				t.Errorf("%s with %s moved runs under %s, want %s for the frozen budgets %+v", operation, name, got, want, frozen.Budgets)
			}
		}
	}
}

// An apply's calls to the machine's controller run outside its budgets, so the
// margin allows each its bound beside the hour for the rest of the media work:
// the pre-boot power read, the insert, the power-off, boot selection and
// power-on that boot the installer (boot.yml and the libvirt boot_media entry
// point), the eject, disk selection and power-on that boot the installed
// system (await.yml and the boot_disk entry point), and the eject the
// verification repeats (apply.yml). A margin shorter than those calls lets
// the runner kill an installation still inside them, so the figures the
// specification states are the ones derived here.
func TestTheMarginAllowsEveryControllerCallAnApplyMakes(t *testing.T) {
	calls := substrate.ControllerPowerReadBound + substrate.ControllerInsertBound + 3*substrate.ControllerPowerBound +
		2*substrate.ControllerBootSelectionBound + 2*substrate.ControllerEjectBound
	if mediaMargin < time.Hour+calls {
		t.Fatalf("the margin is %s, less than an hour and the %s the controller calls may take", mediaMargin, calls)
	}
	if want := time.Hour + 4*time.Minute + 20*time.Second; calls != want {
		t.Errorf("the controller calls may take %s, want the %s the specification states", calls, want)
	}
	request, _ := onlyRequest(t, labCatalog())
	if got, want := request.Deadline(), 4*time.Hour+9*time.Minute+20*time.Second; got != want {
		t.Errorf("lab-rhel installs under %s, want the %s the specification states", got, want)
	}
}
