package installation

import (
	"context"
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

func TestUnreachableRegisteredGuestFailsClosedNamingTheStart(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	released := RegistrationEvidence{}
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: encode(t, Evidence{Absent: true, Postcondition: true, Registration: released, Request: "digest"})}}
	result, err := New(runner).Destroy(context.Background(), encryptionExecution(t, request, nil))
	if reported := diagnostics.Of(err); result.Outcome != reconciliation.OutcomeUnknown || len(reported) != 1 || reported[0].Message != releasedRefusal {
		t.Fatalf("a removal proving no release = %+v, %#v", result, reported)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("the destroy reached the adapter %d times", len(runner.requests))
	}
	want := map[string][2]string{
		"registration-unreachable": {
			"Machine/rhel-01 did not answer through its identity channel or its pinned connection, so the subscription registration its installation created was not removed",
			"start Machine/rhel-01 with bootwright machine start --context lab --name rhel-01, then run bootwright destroy --context lab again",
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
	plain := &fakeRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: encode(t, Evidence{Absent: true, Postcondition: true, Request: "digest"})}}
	result, err = New(plain).Destroy(context.Background(), encryptionExecution(t, pinnedRequest(t, labCatalog()), nil))
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || len(plain.requests) != 1 || len(plain.requests[0].Refusals) != 0 {
		t.Fatalf("a non-registering destroy = %+v (%v), refusals %v", result, diagnostics.Of(err), plain.requests)
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
		"withdrawn and unread":          {registering, Evidence{}, reconciliation.EffectNoEffect},
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
			observation, err := New(runner).ObserveRemoval(context.Background(), registrationExecution(t, test.request, []byte(organizationSentinel), []byte(activationSentinel)))
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
