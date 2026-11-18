package clients

import (
	"slices"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// ProvedEgress lends the route already frozen by a completed controller stage.
// It reads neither current desired state nor the process environment.
func ProvedEgress(machine string, records []lifecycle.BlockEvidence) (prerequisites.SetupEgress, error) {
	var route *prerequisites.SetupEgress
	seen := map[string]bool{}
	for _, record := range records {
		block := record.Block
		if block.Implementation != Implementation {
			continue
		}
		if seen[block.ID] || block.ContentDigest != ContentDigest() {
			return prerequisites.SetupEgress{}, unprovedEgress()
		}
		seen[block.ID] = true
		request, err := DecodeRequest(block.Request)
		if err != nil || request.Machine != machine {
			return prerequisites.SetupEgress{}, unprovedEgress()
		}
		digest, err := reconciliation.RequestDigest(block.BlockDefinition)
		if err != nil || digest != block.RequestDigest || record.State != reconciliation.BlockDone || record.Verb != reconciliation.Apply ||
			block.Kind != Kind || block.Stage != reconciliation.StageController || record.Kind != block.Kind || record.Object != block.Object || record.Implementation != block.Implementation {
			return prerequisites.SetupEgress{}, unprovedEgress()
		}
		evidence, err := reconciliation.DecodeEvidence[Evidence](record.Evidence, maxEvidenceBytes, "controller prerequisites")
		if err != nil || evidence.Retained || evidence.Request != digest {
			return prerequisites.SetupEgress{}, unprovedEgress()
		}
		if _, err := controller.NewProxySelector(request.Egress.HTTPProxy, request.Egress.HTTPSProxy, request.Egress.NoProxy); err != nil {
			return prerequisites.SetupEgress{}, unprovedEgress()
		}
		if route != nil && (route.HTTPProxy != request.Egress.HTTPProxy || route.HTTPSProxy != request.Egress.HTTPSProxy || !slices.Equal(route.NoProxy, request.Egress.NoProxy)) {
			return prerequisites.SetupEgress{}, unprovedEgress()
		}
		value := request.Egress
		value.NoProxy = slices.Clone(value.NoProxy)
		route = &value
	}
	if route == nil {
		return prerequisites.SetupEgress{}, unprovedEgress()
	}
	return *route, nil
}

func unprovedEgress() error {
	return clientsEgressUnproved.Fail("the completed frozen controller stage does not prove one acquisition route", "complete the controller stage and retry apply without changing its route")
}
