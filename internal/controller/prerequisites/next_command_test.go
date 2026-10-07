package prerequisites

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// otherHost is an installed identity other than the one newFixture gives.
func otherHost(t *testing.T) controller.InstalledHostIdentity {
	t.Helper()
	identity, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, strings.Repeat("2", 32), "11111111-2222-4333-8444-555555555555", "22222222-3333-4444-8555-666666666666")
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

// confirmationRefusal is the refusal the CLI's confirmer returns for setup
// (internal/cli/confirmation.go): its remedy repeats the operator's own
// invocation with --yes, which is no command to run as it stands.
func confirmationRefusal(reason string) error {
	return diagnostics.NewFailureWithRemediation("controller.setup", "setup confirmation "+reason, "", "review the plan, then repeat bootwright setup --purge-old-bundles with --yes")
}

// A refused setup's result offers as its next command exactly the bootwright
// command its remediation names, and none when its remediation names none, so
// the result never contradicts the diagnostic printed beside it (B179).
func TestEverySetupRefusalNamesTheCommandItsRemedyGives(t *testing.T) {
	for _, test := range []struct {
		name string
		next string
		run  func(t *testing.T) (*Report, error)
	}{
		{name: "the bound", next: purgeInvocation, run: func(t *testing.T) (*Report, error) {
			f, _, _ := fullHost(t)
			return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		}},
		{name: "an unresumable receipt", next: purgeInvocation, run: func(t *testing.T) (*Report, error) {
			f, _, _ := strandedAtTheBound(t, true)
			movedExecutable(t, f)
			return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		}},
		{name: "the bound client areas hold", run: func(t *testing.T) (*Report, error) {
			f, current, _ := fullHost(t)
			f.store.state.RetainedDefinitions = slices.DeleteFunc(f.store.state.RetainedDefinitions,
				func(definition Definition) bool { return definition.CatalogDigest != current })
			return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
		}},
		{name: "a failed retirement", next: purgeInvocation, run: func(t *testing.T) (*Report, error) {
			f := newFixture(t)
			if _, err := f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			supersede(f, 1)
			f.store.retireErr = diagnostics.NewFailure("context.state", "controller retirement requires an initialized record", "")
			return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true, PurgeOldBundles: true})
		}},
		{name: "a declined confirmation", run: func(t *testing.T) (*Report, error) {
			f := newFixture(t)
			f.confirmationError = confirmationRefusal("was declined; nothing changed")
			return f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true})
		}},
		{name: "a non-interactive confirmation", run: func(t *testing.T) (*Report, error) {
			f := newFixture(t)
			f.confirmationError = confirmationRefusal("requires an interactive terminal")
			return f.service.Setup(context.Background(), SetupRequest{PurgeOldBundles: true})
		}},
		{name: "a change after confirmation", next: setupInvocation, run: func(t *testing.T) (*Report, error) {
			f := newFixture(t)
			f.beforeMutation = func() { f.host.identity = otherHost(t) }
			return f.service.Setup(context.Background(), SetupRequest{SkipConfirmation: true})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			report, err := test.run(t)
			reported := diagnostics.Of(err)
			if len(reported) != 1 {
				t.Fatalf("the refusal carries %d diagnostics: %v %#v", len(reported), err, reported)
			}
			remediation := reported[0].Remediation
			if report == nil {
				t.Fatalf("the refusal %q returned no result", reported[0].Message)
			}
			if report.Next != test.next {
				t.Fatalf("next = %q, want %q beside the remediation %q", report.Next, test.next, remediation)
			}
			if test.next != "" && !strings.Contains(remediation, "run "+test.next) {
				t.Fatalf("next %q is not the command the remediation %q names", test.next, remediation)
			}
			if test.next == "" && strings.Contains(remediation, "run bootwright") {
				t.Fatalf("the remediation %q names a command the result does not offer", remediation)
			}
		})
	}
}
