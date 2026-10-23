package baremetal

import "github.com/crmarques/bootwright/internal/reconciliation"

func ProvedPresence(block reconciliation.Block, data []byte) (Evidence, bool, error) {
	if block.Implementation != Implementation {
		return Evidence{}, false, nil
	}
	request, err := DecodeRequest(block.Request)
	if err != nil {
		return Evidence{}, true, err
	}
	if request.Identity.Object != block.Object || request.Identity.Block != block.ID {
		return Evidence{}, true, refusal("lifecycle.state", "physical Machine proof names another Machine block", "")
	}
	if err := ValidatePresence(data, request, block.RequestDigest); err != nil {
		return Evidence{}, true, err
	}
	evidence, err := decodeEvidence(data, block.RequestDigest)
	return evidence, true, err
}
