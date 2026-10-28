package installation

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

func withPolicy(profile api.Object, policy string) api.Object {
	return profile.WithSpec(profile.Spec().With("updates", api.MapValue(text("policy", policy))))
}

func updatingProfile() api.Object { return withPolicy(registeringProfile(), "withinRelease") }

func updateEvidence(t *testing.T, request Request, marker string, update UpdateEvidence, change func(*Evidence)) []byte {
	t.Helper()
	var evidence Evidence
	if err := json.Unmarshal(completeEvidence(request, "digest", marker), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Registration = RegistrationEvidence{Consumer: ownedConsumer, State: registrationOwned}
	evidence.Update = update
	if change != nil {
		change(&evidence)
	}
	return encode(t, evidence)
}

func TestTheUpdateFreezesItsReleaseUnlessThePolicyIsNone(t *testing.T) {
	updating := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	if updating.Update == nil || *updating.Update != (Update{Policy: "withinRelease", Release: "9.8"}) {
		t.Fatalf("update = %+v", updating.Update)
	}
	defaulted := installProfile()
	defaulted = defaulted.WithSpec(defaulted.Spec().Without("updates"))
	if request := pinnedRequest(t, labCatalog(defaulted)); request.Update == nil || request.Update.Release != "9.8" {
		t.Fatalf("an omitted policy freezes %+v, want the withinRelease default", request.Update)
	}
	if none := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement())); none.Update != nil {
		t.Fatalf("a none profile freezes %+v", none.Update)
	}
	canonical, err := updating.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(canonical), `"update":{"policy":"withinRelease","release":"9.8"}`) {
		t.Fatalf("the canonical request carries no update: %s", canonical)
	}
	none := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	if canonical, _ := none.Canonical(); strings.Contains(string(canonical), `"update"`) {
		t.Fatalf("a none request freezes an update: %s", canonical)
	}
}

func TestAReleaseOutsideTheGrammarIsNotFrozen(t *testing.T) {
	for _, release := range []string{"9", "9.08", "9.8.1", "9.8'", "10.x"} {
		profile := updatingProfile()
		profile = profile.WithSpec(profile.Spec().WithPath(api.StringValue(release), "os", "version"))
		_, err := updateFor(profile)
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.unsupported" ||
			!strings.Contains(reported[0].Message, "updates within release "+release+", which is not a <major>.<minor> release") {
			t.Fatalf("%q: refusal = %#v", release, reported)
		}
	}
	if update, err := updateFor(withPolicy(updatingProfile(), "none")); update != nil || err != nil {
		t.Fatalf("a none profile = %+v (%v)", update, err)
	}
}

func TestTheReleaseVariablePinsReleaseverInPost(t *testing.T) {
	updating := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	want := strings.Join([]string{
		"install -d -m 0755 /etc/dnf/vars",
		"printf '%s\\n' '9.8' > /etc/dnf/vars/releasever",
		"chown root:root /etc/dnf/vars/releasever",
		"chmod 0644 /etc/dnf/vars/releasever",
	}, "\n")
	post := updating.Kickstart[strings.Index(updating.Kickstart, "%post --erroronfail\nset -eu"):]
	if !strings.Contains(post, want) || strings.Index(post, want) > strings.Index(post, "/etc/sudoers.d/60-bootwright") {
		t.Fatalf("the %%post does not write the release variable before the sudoers drop-in:\n%s", post)
	}
	none := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	if strings.Contains(none.Kickstart, "/etc/dnf/vars") {
		t.Fatal("a none profile writes the release variable")
	}
}

func TestTheUpdateBoundCoversItsSessionsAndItsReboot(t *testing.T) {
	sessions := 1800*time.Second + updateRebootBound + 120*time.Second + 120*time.Second + 60*time.Second
	if updateBound != 45*time.Minute || sessions != updateBound {
		t.Fatalf("the update bound is %s for %s of sessions and reboot, want 45m", updateBound, sessions)
	}
	updating := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	registered := updating
	registered.Update = nil
	if got := updating.Deadline() - registered.Deadline(); got != updateBound+substrate.LibvirtBootDiskBound {
		t.Fatalf("an update adds %s, want the bound and the disk boot's %s", got, substrate.LibvirtBootDiskBound)
	}
	unregistered := updating
	unregistered.Registration = nil
	for name, test := range map[string]struct {
		request Request
		want    time.Duration
	}{
		"updating":             {unregistered, 5*time.Hour + 41*time.Minute + 50*time.Second},
		"registering updating": {updating, 5*time.Hour + 56*time.Minute + 50*time.Second},
	} {
		if got := test.request.Deadline(); got != test.want || got > lifecycle.MaxDeadline {
			t.Errorf("a %s lab-rhel installs under %s, want %s within %s", name, got, test.want, lifecycle.MaxDeadline)
		}
		for _, operation := range []string{"apply", "observe", "observe-removal", "destroy"} {
			if deadline := runDeadline(t, test.request, operation); deadline != test.want {
				t.Errorf("a %s %s states %s, want %s", name, operation, deadline, test.want)
			}
		}
	}
}

func TestAPhysicalUpdateAddsItsBoundAndThePhysicalDiskBoot(t *testing.T) {
	machine := eraseMachine()
	catalog := labCatalog(metalProvider(), machine, sshKeySecret(api.MapValue(field("generated", api.MapValue()))), updatingProfile(), rhelEntitlement())
	request := requestOf(t, catalog, machine.Name())
	if request.Update == nil || request.Registration == nil {
		t.Fatalf("the physical fixture neither updates nor registers: %+v", request)
	}
	unchanged := request
	unchanged.Update = nil
	bounds, _ := substrate.MachineBoundsFor(substrate.ArmBaremetal)
	if got := request.Deadline() - unchanged.Deadline(); got != updateBound+bounds.BootDisk {
		t.Fatalf("a physical update adds %s, want %s", got, updateBound+bounds.BootDisk)
	}
}

func TestApplyNamesEveryUpdateRefusalWithItsDetail(t *testing.T) {
	request := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	runner := &fakeRunner{}
	_, _ = New(runner).Apply(context.Background(), registrationExecution(t, request, []byte("org"), []byte("key")))
	if len(runner.requests) != 1 {
		t.Fatal("apply did not reach the adapter")
	}
	refusals := runner.requests[0].Refusals
	machine, profile := "Machine/rhel-01", "MachineInstallProfile/rhel-9-8"
	cases := []struct{ reason, detail, message, remediation string }{
		{"update-release-unavailable", "", "the subscription service offers no release 9.8 to " + machine,
			"set spec.os.version on " + profile + " to a release the subscription service offers, or set spec.updates.policy to none, then run bootwright destroy --context lab and apply again"},
		{"update-release-mismatch", "9.6", machine + " reports VERSION_ID 9.6, and its installation declares release 9.8",
			"run bootwright destroy --context lab, make the update sources of " + profile + " offer release 9.8 alone, and apply again"},
		{"update-release-mismatch", "9 6", machine + " reports VERSION_ID unknown, and its installation declares release 9.8",
			"run bootwright destroy --context lab, make the update sources of " + profile + " offer release 9.8 alone, and apply again"},
		{"update-release-drift", "9.9@rhel-9-for-x86_64-baseos-rpms",
			"the update of " + machine + " would move redhat-release to 9.9 from repository rhel-9-for-x86_64-baseos-rpms, outside release 9.8",
			"pin repository rhel-9-for-x86_64-baseos-rpms of " + profile + " to release 9.8 or disable it, then run bootwright destroy --context lab and apply again"},
		{"update-release-drift", "9.9", "the update of " + machine + " would move redhat-release to unknown from repository unknown, outside release 9.8",
			"pin repository unknown of " + profile + " to release 9.8 or disable it, then run bootwright destroy --context lab and apply again"},
		{"update-no-repository", "", machine + " has no enabled repository to update release 9.8 from",
			"enable a repository for " + profile + " or set spec.updates.policy to none, then run bootwright destroy --context lab and apply again"},
		{"update-foreign", "", machine + " holds an update record of another installation", "run bootwright destroy --context lab and apply again"},
		{"update-unavailable", "metadata", "a repository did not answer while " + machine + " updated within release 9.8: metadata",
			"restore the route from " + machine + " to its update sources, then run bootwright apply --context lab again"},
		{"update-unavailable", "transaction", "a repository did not answer while " + machine + " updated within release 9.8: unknown",
			"restore the route from " + machine + " to its update sources, then run bootwright apply --context lab again"},
		{"update-failed", "transaction", "the package transaction of " + machine + " failed while it updated within release 9.8: transaction",
			"run bootwright apply --context lab again once the update sources of " + profile + " offer a consistent transaction, or run bootwright destroy --context lab and set spec.updates.policy to none"},
	}
	for _, test := range cases {
		named, ok := refusals[test.reason]
		if !ok {
			t.Fatalf("apply names no %s refusal: %v", test.reason, slices.Sorted(maps.Keys(refusals)))
		}
		completed := named
		if test.detail != "" {
			completed = named.(interface{ WithDetail(string) error }).WithDetail(test.detail)
		}
		reported := diagnostics.Of(completed)
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Object == nil || reported[0].Object.Name != "rhel-01" ||
			reported[0].Message != test.message || reported[0].Remediation != test.remediation {
			t.Errorf("%s with %q = %#v", test.reason, test.detail, reported)
		}
	}
	if updateRefusals(testContext, pinnedRequest(t, labCatalog())) != nil {
		t.Fatal("a none request names update refusals")
	}
}

func TestCompletionRequiresTheUpdateItsPolicyNames(t *testing.T) {
	updating := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(updating, "digest")
	complete := completedUpdate(updating)
	if err := ValidatePresence(updateEvidence(t, updating, string(marker), complete, nil), updating, "digest", string(marker)); err != nil {
		t.Fatalf("a complete update refused: %v", diagnostics.Of(err))
	}
	unchanged := UpdateEvidence{Kernel: complete.Kernel, Release: "9.8", State: updateComplete}
	if err := ValidatePresence(updateEvidence(t, updating, string(marker), unchanged, nil), updating, "digest", string(marker)); err != nil {
		t.Fatalf("a complete update that changed nothing refused: %v", diagnostics.Of(err))
	}
	otherRelease := complete
	otherRelease.Release = "9.7"
	for _, update := range []UpdateEvidence{{}, {State: updateNone}, {State: updateAbsent}, {State: updateForeign},
		{Kernel: complete.Kernel, Release: "9.8", State: updatePending},
		{Kernel: complete.Kernel, Packages: 212, Release: "9.8", State: updateUpgraded, Transaction: 3}, otherRelease} {
		err := ValidatePresence(updateEvidence(t, updating, string(marker), update, nil), updating, "digest", string(marker))
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the installed machine has not completed its update within release 9.8" {
			t.Fatalf("%+v: refusal = %#v", update, reported)
		}
	}
	none := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	noneMarker, _ := MarkerFor(none, "digest")
	if err := ValidatePresence(updateEvidence(t, none, string(noneMarker), UpdateEvidence{State: updateNone}, nil), none, "digest", string(noneMarker)); err != nil {
		t.Fatalf("a none request refused state none: %v", diagnostics.Of(err))
	}
	err := ValidatePresence(updateEvidence(t, none, string(noneMarker), completedUpdate(updating), nil), none, "digest", string(noneMarker))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the installation evidence reports an update its request does not perform" {
		t.Fatalf("a none request with an update: refusal = %#v", reported)
	}
}

func TestAnUnfinishedUpdateIsAPositivePartialAndAForeignOneStaysUnknown(t *testing.T) {
	request := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	kernel := "5.14.0-570.12.1.el9_6.x86_64"
	for _, update := range []UpdateEvidence{{}, {State: updateAbsent}, {Kernel: kernel, Release: "9.8", State: updatePending},
		{Kernel: kernel, Packages: 4, Release: "9.8", State: updateUpgraded, Transaction: 7}} {
		evidence := updateEvidence(t, request, string(marker), update, unfinished)
		if effect, err := installationEffect(reconciliation.Apply, evidence, request, "digest", string(marker)); effect != reconciliation.EffectPartial {
			t.Fatalf("%+v: effect = %s (%v)", update, effect, diagnostics.Of(err))
		}
	}
	evidence := updateEvidence(t, request, string(marker), UpdateEvidence{State: updateForeign}, unfinished)
	effect, err := installationEffect(reconciliation.Apply, evidence, request, "digest", string(marker))
	if reported := diagnostics.Of(err); effect != reconciliation.EffectUnknown || len(reported) != 1 ||
		reported[0].Message != "the installed machine holds an update record of another installation" {
		t.Fatalf("a foreign update: effect = %s (%#v)", effect, reported)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: request.Identity.Block, Object: "rhel-01", Request: canonical}, RequestDigest: "digest"}
	unresolved, explains := New(nil).Unresolved(reconciliation.Apply, block, evidence)
	if !explains || !strings.Contains(unresolved.Reason, "the installed machine holds an update record of another installation") {
		t.Fatalf("unresolved = %+v (%t)", unresolved, explains)
	}
}

func TestMalformedUpdateEvidenceIsRefused(t *testing.T) {
	request := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	kernel := "5.14.0-570.12.1.el9_6.x86_64"
	for _, update := range []UpdateEvidence{
		{State: "registered"}, {State: updateNone, Release: "9.8"}, {State: updateAbsent, Transaction: 1},
		{State: updateForeign, Kernel: kernel}, {State: "", Rebooted: true},
		{Kernel: kernel, Release: "9", State: updatePending}, {Release: "9.8", State: updatePending},
		{Kernel: "a b", Release: "9.8", State: updatePending}, {Kernel: kernel, Release: "9.8", State: updateUpgraded, Transaction: -1},
		{Kernel: kernel, Release: "9.8", State: updateUpgraded, Packages: -1},
		{Kernel: kernel, Release: "9.8", State: updateUpgraded, Transaction: 3, Rebooted: true},
		{Kernel: kernel, Release: "9.8", State: updateComplete, Transaction: 3},
		{Kernel: kernel, Release: "9.8", State: updateComplete, Rebooted: true},
	} {
		_, err := decodeEvidence(updateEvidence(t, request, string(marker), update, nil), "digest")
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the installation evidence reports a malformed update" {
			t.Fatalf("%+v: refusal = %#v", update, reported)
		}
	}
	for _, update := range []UpdateEvidence{{}, {State: updateNone}, {State: updateAbsent}, {State: updateForeign}, completedUpdate(request),
		{Kernel: kernel, Release: "10.0", State: updatePending}} {
		if _, err := decodeEvidence(updateEvidence(t, request, string(marker), update, nil), "digest"); err != nil {
			t.Fatalf("%+v refused: %v", update, diagnostics.Of(err))
		}
	}
}

func TestAPolicyChangeMovesTheInstallationRequest(t *testing.T) {
	updating, err := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement())).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	none, err := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement())).Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if string(updating) == string(none) {
		t.Fatal("a policy change kept the frozen request")
	}
}
