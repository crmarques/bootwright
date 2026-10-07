package managedservice

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Evidence this request's own that none of the verb's checks accept is named
// by the check that decided: under an apply a service claiming its
// postcondition with its unit failed, refused by the presence check; under a
// removal an observation that reports nothing at all without the absence form,
// refused by the unfinished removal's. Evidence for another request and no
// evidence at all are left to the general reason.
func TestUnresolvedNamesTheRefusingCheck(t *testing.T) {
	catalog := catalogOf(controller(), service(api.Proxy, "lab-proxy"))
	plan, err := NewCapability(testDefinition(), nil).Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
		State: compilation.NewState(catalog, catalog, nil), Controller: "controller",
	})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	block := frozen.Blocks[0]
	request, err := DecodeRequest(block.Request, testDefinition().Version)
	if err != nil {
		t.Fatal(err)
	}
	digest := block.RequestDigest
	failed := Evidence{Answers: []Answer{}, Container: request.Image, ContentRoot: true, Postcondition: true, Request: digest, Unit: "failed"}
	other := failed
	other.Request = strings.Repeat("e", 64)
	empty := Evidence{Answers: []Answer{}, Request: digest}
	subject := "Proxy lab-proxy on Machine controller"
	for name, test := range map[string]struct {
		verb     reconciliation.Verb
		evidence []byte
		want     lifecycle.Unresolved
		explains bool
	}{
		"an apply over a failed unit": {reconciliation.Apply, encode(t, failed), lifecycle.Unresolved{
			Reason: subject + " is not what its apply froze: the managed service unit is not active",
			Remedy: "restore " + subject + " to what its frozen request names",
		}, true},
		"a removal over an observation reporting nothing": {reconciliation.Destroy, encode(t, empty), lifecycle.Unresolved{
			Reason: subject + " is not what its destroy froze: the managed service evidence reports nothing its removal takes back",
			Remedy: "restore " + subject + " so its observation reads it as this context's own",
		}, true},
		"a removal over a failed unit": {reconciliation.Destroy, encode(t, failed), lifecycle.Unresolved{}, false},
		"evidence for another request": {reconciliation.Apply, encode(t, other), lifecycle.Unresolved{}, false},
		"no evidence":                  {reconciliation.Apply, nil, lifecycle.Unresolved{}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, explains := NewCapability(testDefinition(), nil).Unresolved(test.verb, block, test.evidence)
			if explains != test.explains || got != test.want {
				t.Fatalf("unresolved = %+v (%t), want %+v (%t)", got, explains, test.want, test.explains)
			}
		})
	}
}
