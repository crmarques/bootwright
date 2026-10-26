package installation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const releasedRefusal = "the installation removal did not prove its machine holds no subscription registration it created"

func registeringBlock(t *testing.T, request Request) reconciliation.Block {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: request.Identity.Block, Object: request.Identity.Object, Request: canonical}, RequestDigest: "digest"}
}

// proving hands the execution the preparation its apply records when it proved
// the frozen marker before it registered.
func proving(t *testing.T, execution lifecycle.Execution, request Request) lifecycle.Execution {
	t.Helper()
	marker, err := MarkerFor(request, execution.Block.RequestDigest)
	if err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(string(marker))
	execution.ApplyPreparations = []json.RawMessage{json.RawMessage(`{"marker":` + string(quoted) + `}`)}
	return execution
}

func removalRunner(t *testing.T, registration RegistrationEvidence) *fakeRunner {
	return &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: encode(t, Evidence{Absent: true, Postcondition: true, Registration: registration, Request: "digest"})}}
}

func TestUnreachableRegisteredGuestFailsClosedNamingTheStart(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	runner := removalRunner(t, RegistrationEvidence{})
	result, err := New(runner).Destroy(context.Background(), proving(t, encryptionExecution(t, request, nil), request))
	if reported := diagnostics.Of(err); result.Outcome != reconciliation.OutcomeUnknown || len(reported) != 1 || reported[0].Message != releasedRefusal {
		t.Fatalf("a removal proving no release = %+v, %#v", result, reported)
	}
	if len(runner.requests) != 1 || runner.requests[0].MaterialValues["unregister"] != "guest" {
		t.Fatalf("the destroy reached the adapter as %+v", runner.requests)
	}
	want := map[string][2]string{
		"registration-unreachable": {
			"the apply of Machine/rhel-01 proved its install marker before it registered, and Machine/rhel-01 did not answer through its identity channel or its pinned connection, so its subscription registration cannot be read and was not removed",
			"start Machine/rhel-01 with bootwright machine start --context lab --name rhel-01, then run bootwright destroy --context lab again; if Machine/rhel-01 cannot answer again, remove its consumer with the subscription service's own tools, then run bootwright destroy --context lab --unregistered rhel-01",
		},
		"registration-unavailable": {
			"the subscription service, or the registration tooling on Machine/rhel-01, did not complete the removal of the subscription consumer its installation created",
			"restore the route from Machine/rhel-01 to the subscription service, then run bootwright destroy --context lab again",
		},
	}
	refusals := runner.requests[0].Refusals
	if len(refusals) != len(want) {
		t.Fatalf("the destroy names refusals %v", refusals)
	}
	for reason, text := range want {
		reported := diagnostics.Of(refusals[reason])
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != text[0] || reported[0].Remediation != text[1] ||
			reported[0].Object == nil || reported[0].Object.Kind != "Machine" || reported[0].Object.Name != "rhel-01" {
			t.Fatalf("%s = %#v", reason, reported)
		}
	}
	plain := removalRunner(t, RegistrationEvidence{})
	result, err = New(plain).Destroy(context.Background(), encryptionExecution(t, pinnedRequest(t, labCatalog()), nil))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(plain.requests) != 1 || len(plain.requests[0].Refusals) != 0 {
		t.Fatalf("a non-registering destroy = %+v (%v), refusals %v", result, diagnostics.Of(err), plain.requests)
	}
	if _, decided := plain.requests[0].MaterialValues["unregister"]; decided {
		t.Fatalf("a non-registering destroy is told %q", plain.requests[0].MaterialValues["unregister"])
	}
}

func TestProvedMarkerWithUnreadableRegistrationRefusesWithTheManualPath(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	runner := &fakeRunner{err: errors.New("unreachable")}
	_, _ = New(runner).Destroy(context.Background(), proving(t, encryptionExecution(t, request, nil), request))
	if len(runner.requests) != 1 {
		t.Fatalf("the destroy reached the adapter %d times", len(runner.requests))
	}
	reported := diagnostics.Of(runner.requests[0].Refusals["registration-unreachable"])
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Object == nil || reported[0].Object.Kind != "Machine" || reported[0].Object.Name != "rhel-01" ||
		reported[0].Message != "the apply of Machine/rhel-01 proved its install marker before it registered, and Machine/rhel-01 did not answer through its identity channel or its pinned connection, so its subscription registration cannot be read and was not removed" ||
		reported[0].Remediation != "start Machine/rhel-01 with bootwright machine start --context lab --name rhel-01, then run bootwright destroy --context lab again; if Machine/rhel-01 cannot answer again, remove its consumer with the subscription service's own tools, then run bootwright destroy --context lab --unregistered rhel-01" {
		t.Fatalf("registration-unreachable = %#v", reported)
	}
}

func TestNeverPreparedRegistrationIsDestroyedWithoutUnregistration(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	runner := removalRunner(t, RegistrationEvidence{})
	result, err := New(runner).Destroy(context.Background(), encryptionExecution(t, request, nil))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged {
		t.Fatalf("a never-prepared destroy = %+v (%v)", result, diagnostics.Of(err))
	}
	if len(runner.requests) != 1 || runner.requests[0].MaterialValues["unregister"] != "skip" || len(runner.requests[0].Refusals) != 2 {
		t.Fatalf("the destroy reached the adapter as %+v", runner.requests)
	}
	evidence := encode(t, Evidence{Absent: true, Postcondition: true, Request: "digest"})
	if err := ValidateAbsence(evidence, "digest"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReleased(evidence, request, "digest", false); err != nil {
		t.Fatalf("an unread registration of a never-registered installation = %v", diagnostics.Of(err))
	}
	read := removalRunner(t, RegistrationEvidence{State: "absent"})
	if _, err := New(read).Destroy(context.Background(), encryptionExecution(t, request, nil)); err == nil {
		t.Fatal("a removal told to skip the guest accepted a registration it read")
	}
}

func TestProvedMarkerWithReadableRegistrationUnregisters(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	runner := removalRunner(t, RegistrationEvidence{Consumer: ownedConsumer, State: "unregistered"})
	result, err := New(runner).Destroy(context.Background(), proving(t, encryptionExecution(t, request, nil), request))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(runner.requests) != 1 || runner.requests[0].MaterialValues["unregister"] != "guest" {
		t.Fatalf("a proved destroy = %+v (%v), adapter %+v", result, diagnostics.Of(err), runner.requests)
	}
	unread := removalRunner(t, RegistrationEvidence{})
	result, err = New(unread).Destroy(context.Background(), proving(t, encryptionExecution(t, request, nil), request))
	if reported := diagnostics.Of(err); result.Outcome != reconciliation.OutcomeUnknown || len(reported) != 1 || reported[0].Message != releasedRefusal {
		t.Fatalf("a proved destroy that read nothing = %+v, %#v", result, reported)
	}
}

func TestReleasedMachineSkipsTheGuest(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	execution := proving(t, encryptionExecution(t, request, nil), request)
	execution.Released = true
	runner := removalRunner(t, RegistrationEvidence{})
	result, err := New(runner).Destroy(context.Background(), execution)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(runner.requests) != 1 || runner.requests[0].MaterialValues["unregister"] != "skip" {
		t.Fatalf("a released destroy = %+v (%v), adapter %+v", result, diagnostics.Of(err), runner.requests)
	}
	observer := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, Evidence{Request: "digest"})}}
	observation, err := New(observer).ObserveRemoval(context.Background(), execution)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || observer.requests[0].MaterialValues["unregister"] != "skip" {
		t.Fatalf("a released removal observation = %+v (%v), adapter %+v", observation, err, observer.requests)
	}
}

func TestForeignPreparationRefusesBeforeTheRun(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	for name, observe := range map[string]bool{"destroy": false, "removal observation": true} {
		t.Run(name, func(t *testing.T) {
			execution := encryptionExecution(t, request, nil)
			execution.ApplyPreparations = []json.RawMessage{json.RawMessage(`{"marker":"another"}`)}
			runner := removalRunner(t, RegistrationEvidence{})
			var err error
			if observe {
				_, err = New(runner).ObserveRemoval(context.Background(), execution)
			} else {
				_, err = New(runner).Destroy(context.Background(), execution)
			}
			reported := diagnostics.Of(err)
			if len(runner.requests) != 0 || len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
				reported[0].Message != "the apply records of Machine/rhel-01 hold a before-state this executable cannot read" {
				t.Fatalf("a foreign preparation = %#v, adapter %+v", reported, runner.requests)
			}
		})
	}
}

func TestRemovalResolutionReadsTheGuestOnlyWhenRegistrationIsFrozen(t *testing.T) {
	registering := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	plain := pinnedRequest(t, labCatalog())
	for name, test := range map[string]struct {
		request  Request
		evidence Evidence
		want     reconciliation.EffectState
	}{
		"withdrawn and absent":          {registering, Evidence{Registration: RegistrationEvidence{State: "absent"}}, reconciliation.EffectCompleted},
		"withdrawn and unregistered":    {registering, Evidence{Registration: RegistrationEvidence{Consumer: ownedConsumer, State: "unregistered"}}, reconciliation.EffectCompleted},
		"withdrawn and retained":        {registering, Evidence{Registration: RegistrationEvidence{State: "retained"}}, reconciliation.EffectCompleted},
		"withdrawn and foreign":         {registering, Evidence{Registration: RegistrationEvidence{State: "foreign"}}, reconciliation.EffectCompleted},
		"withdrawn and still owned":     {registering, Evidence{Registration: RegistrationEvidence{Consumer: ownedConsumer, State: "owned"}}, reconciliation.EffectPartial},
		"withdrawn and unread":          {registering, Evidence{}, reconciliation.EffectInconclusive},
		"content left and unread":       {registering, Evidence{Image: true}, reconciliation.EffectPartial},
		"not registering, withdrawn":    {plain, Evidence{}, reconciliation.EffectCompleted},
		"not registering, content left": {plain, Evidence{Image: true}, reconciliation.EffectPartial},
		"registering, another request":  {registering, Evidence{Registration: RegistrationEvidence{State: "absent"}, Request: "another"}, reconciliation.EffectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			if test.evidence.Request == "" {
				test.evidence.Request = "digest"
			}
			runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "unchanged", Evidence: encode(t, test.evidence)}}
			execution := proving(t, registrationExecution(t, test.request, []byte(organizationSentinel), []byte(activationSentinel)), test.request)
			observation, err := New(runner).ObserveRemoval(context.Background(), execution)
			if err != nil || observation.Effect != test.want {
				t.Fatalf("removal observation = %+v (%v), want %s", observation, err, test.want)
			}
			if len(runner.requests) != 1 || runner.requests[0].Operation != "observe" || runner.requests[0].MaterialValues["observes"] != "removal" {
				t.Fatalf("adapter invocation = %+v", runner.requests)
			}
			if slices.ContainsFunc(runner.requests[0].Materials, func(material lifecycle.MaterialFile) bool {
				return material.Secret == "rhel-org" || material.Secret == "rhel-key"
			}) {
				t.Fatalf("the removal observation lends registration material: %+v", runner.requests[0].Materials)
			}
		})
	}
}

func TestOnlyARegisteringInstallationOptsIntoPreStopRemoval(t *testing.T) {
	var capability lifecycle.PreStopRemoval = New(nil)
	if !capability.RequiresPreStopRemoval(registeringBlock(t, pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement())))) {
		t.Fatal("a registering installation does not opt into pre-stop removal")
	}
	if capability.RequiresPreStopRemoval(registeringBlock(t, pinnedRequest(t, labCatalog()))) {
		t.Fatal("a non-registering installation opts into pre-stop removal")
	}
	if capability.RequiresPreStopRemoval(reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: []byte(`{"version":"os-install-anaconda-v1"}`)}}) {
		t.Fatal("an undecodable request opts into pre-stop removal")
	}
}

func groupIDs(groups []reconciliation.Group) []string {
	ids := []string{}
	for _, group := range groups {
		ids = append(ids, group.ID)
	}
	return ids
}

func TestRegisteringRemovalNamesItsUnregistrationFirst(t *testing.T) {
	removal, err := New(nil).Removal(context.Background(), registeringBlock(t, pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(groupIDs(removal.Groups), []string{"unregister-subscription", "remove-published", "verify-absence"}) ||
		removal.Groups[0].Description != "remove the subscription consumer this installation registered the machine with" ||
		!slices.Equal(removal.Groups[0].Machines, []string{"rhel-01"}) {
		t.Fatalf("registering removal groups = %+v", removal.Groups)
	}
	if !slices.Contains(removal.Impacts, "unregister-subscription rhel-01") || !slices.IsSorted(removal.Impacts) || len(removal.Consumes) != 0 ||
		removal.Description != "unregister rhel-01 from its subscription service and remove its installer media" {
		t.Fatalf("registering removal = %+v", removal)
	}
	plain, err := New(nil).Removal(context.Background(), registeringBlock(t, pinnedRequest(t, labCatalog())))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(groupIDs(plain.Groups), []string{"remove-published", "verify-absence"}) || plain.Description != "remove the installer media of rhel-01" ||
		slices.ContainsFunc(plain.Impacts, func(impact string) bool { return impact == "unregister-subscription rhel-01" }) || len(plain.Impacts) != 1 {
		t.Fatalf("non-registering removal = %+v", plain)
	}
}

func TestRemovalEvidenceDecodesTheUnregistrationStates(t *testing.T) {
	for _, registration := range []RegistrationEvidence{{State: "unregistered"}, {Consumer: "not-a-uuid", State: "unregistered"}, {Consumer: ownedConsumer, State: "retained"}} {
		_, err := decodeEvidence(encode(t, Evidence{Absent: true, Postcondition: true, Registration: registration, Request: "digest"}), "digest")
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the installation evidence reports a malformed registration" {
			t.Fatalf("%+v: refusal = %#v", registration, reported)
		}
	}
	for _, registration := range []RegistrationEvidence{{Consumer: ownedConsumer, State: "unregistered"}, {State: "retained"}} {
		if _, err := decodeEvidence(encode(t, Evidence{Absent: true, Postcondition: true, Registration: registration, Request: "digest"}), "digest"); err != nil {
			t.Fatalf("%+v refused: %v", registration, diagnostics.Of(err))
		}
	}
}

// The removal declares itself replay-safe, so the inconclusive reading of a
// registration it could not read admits the one retry the engine allows,
// which reads the guest again before any effect (Q65, D183).
func TestTheInstallationRemovalDeclaresItselfReplaySafe(t *testing.T) {
	call, _ := execution(t, "digest")
	removal, err := New(nil).Removal(context.Background(), call.Block)
	if err != nil || !removal.ReplaySafe {
		t.Fatalf("removal = %+v (%v)", removal, err)
	}
}
