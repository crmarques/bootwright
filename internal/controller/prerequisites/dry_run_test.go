package prerequisites

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A purge previews what it will do: retirement follows the setup it runs
// beside, so the dry run names it after that setup's own actions.
func TestAPurgeDryRunSaysSupersededBundlesWouldBeRetired(t *testing.T) {
	const retirement = "Retire the superseded execution bundles this host holds once setup completes"
	for _, purge := range []bool{false, true} {
		f := newFixture(t)
		report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true, PurgeOldBundles: purge})
		want := setupInvocation
		if purge {
			want = purgeInvocation
		}
		if err != nil || report.Outcome != "planned" || report.Purge != purge || report.Next != want {
			t.Fatalf("purge=%t: %#v %v", purge, report, err)
		}
		if slices.Contains(report.Actions, retirement) != purge || purge && report.Actions[len(report.Actions)-1] != retirement {
			t.Fatalf("purge=%t: the preview's actions are %q", purge, report.Actions)
		}
		if f.store.reads != 0 || f.store.writes != 0 || len(f.store.retired) != 0 {
			t.Fatalf("purge=%t: the preview read %d records, wrote %d and retired %v", purge, f.store.reads, f.store.writes, f.store.retired)
		}
	}
}

// inspectedRoot is a Storage whose state root the dry run can inspect, as the
// context store's can.
type inspectedRoot struct {
	*memoryStorage
	inspection StateRootInspection
	err        error
	calls      int
}

func (r *inspectedRoot) InspectStateRoot(context.Context) (StateRootInspection, error) {
	r.calls++
	return r.inspection, r.err
}

// A dry run reads no record, but it still reports the state root this build
// would use, so it never previews a setup that root would refuse: a root it
// cannot use is a not-ready row and the store's own refusal, with its remedy,
// beside the planned report (B349).
func TestASetupDryRunRefusesAStateRootThisBuildCannotUse(t *testing.T) {
	// The store's refusal of a state root with the wrong owner or mode
	// (internal/workspace/contextfs/repository_linux_amd64.go, unsafeRoot).
	refusal := diagnostics.NewFailureWithRemediation("context.state",
		"the state root is a directory owned by root:root with mode 0755, but it must be a directory owned by root:root with mode 0700", "",
		"nothing repairs it, because Bootwright never changes the owner or mode of an existing state root; another Bootwright build may have created this root")
	for _, test := range []struct {
		name       string
		inspection StateRootInspection
	}{
		{"an absent root", StateRootInspection{Required: "a root:root 0700 directory on a local filesystem", Observed: "absent; setup creates it", Status: "ready"}},
		{"an unlistable root", StateRootInspection{Required: "a root:root 0700 directory on a local filesystem", Observed: "root:root 0700; setup verifies its contents", Status: "ready"}},
		{"a root with the wrong mode", StateRootInspection{Required: "a root:root 0700 directory on a local filesystem", Observed: "a directory owned by root:root with mode 0755", Status: "not-ready", Refusal: refusal}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			root := &inspectedRoot{memoryStorage: &f.store, inspection: test.inspection}
			f.service.storage = root
			report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true})
			if root.calls != 1 || report == nil || report.Outcome != "planned" || !report.DryRun {
				t.Fatalf("the dry run inspected the root %d times and reported %#v", root.calls, report)
			}
			index := slices.IndexFunc(report.Checks, func(check Check) bool { return check.ID == "state-root" })
			if index < 0 {
				t.Fatalf("the dry run reports no state root: %#v", report.Checks)
			}
			check := report.Checks[index]
			if check.Required != test.inspection.Required || check.Observed != test.inspection.Observed || check.Status != test.inspection.Status || check.Scope != HostScope {
				t.Fatalf("the state root reads %#v, want %#v", check, test.inspection)
			}
			if test.inspection.Refusal == nil {
				if err != nil || report.Next != setupInvocation {
					t.Fatalf("a usable root refused (%v) or offered %q", err, report.Next)
				}
			} else {
				reported := diagnostics.Of(err)
				if len(reported) != 1 || reported[0].Message != diagnostics.Of(refusal)[0].Message || reported[0].Remediation != diagnostics.Of(refusal)[0].Remediation {
					t.Fatalf("an unusable root refused with %#v, want the store's own refusal", reported)
				}
				if report.Next != "" {
					t.Fatalf("a remedy that names no command offers %q", report.Next)
				}
			}
			if f.store.reads != 0 || f.store.writes != 0 || f.host.identities != 0 {
				t.Fatalf("the dry run read %d records, wrote %d and verified the host %d times", f.store.reads, f.store.writes, f.host.identities)
			}
		})
	}
	f := newFixture(t)
	stopped := errors.New("the inspection could not run")
	f.service.storage = &inspectedRoot{memoryStorage: &f.store, err: stopped}
	if _, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true}); !errors.Is(err, stopped) {
		t.Fatalf("an inspection that could not run = %v", err)
	}
}
