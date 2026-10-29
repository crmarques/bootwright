package main

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/packages"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
)

// The kubevirt package grants a hosted namespace's ServiceAccount every verb
// the arm uses there, each of which the provider proof reviews, so a token
// minted for it can do what the arm does and nothing that proof would miss.
func TestTheHostedAccountHoldsEveryVerbTheArmReviews(t *testing.T) {
	granted := packages.MachineAccess()
	for _, access := range kubevirt.Verbs() {
		resource := access.Resource
		if access.Subresource != "" {
			resource += "/" + access.Subresource
		}
		if !slices.Contains(granted, [3]string{access.Group, resource, access.Verb}) {
			t.Errorf("the kubevirt package grants no %s %s/%s", access.Verb, access.Group, resource)
		}
	}
	if len(granted) != len(kubevirt.Verbs())+1 {
		t.Fatalf("the package grants %d permissions beside the arm's %d and the attachment read", len(granted), len(kubevirt.Verbs()))
	}
}
