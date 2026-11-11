package hardware

import (
	"regexp"
	"slices"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxEvidenceBytes = 32 << 10

var permanentAddress = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

type Evidence struct {
	Absent        bool     `json:"absent"`
	Addresses     []string `json:"addresses"`
	Postcondition bool     `json:"postcondition"`
	Request       string   `json:"request"`
	Version       string   `json:"version"`
}

func decodeEvidence(data []byte) (Evidence, error) {
	evidence, err := reconciliation.DecodeEvidence[Evidence](data, maxEvidenceBytes, "machine hardware")
	if err != nil {
		return Evidence{}, err
	}
	malformed := refusal("lifecycle.state", "the machine hardware evidence is malformed", "")
	if evidence.Version != EvidenceVersion || len(evidence.Addresses) > MaxAddresses || !slices.IsSorted(evidence.Addresses) ||
		len(slices.Compact(slices.Clone(evidence.Addresses))) != len(evidence.Addresses) || evidence.Absent && len(evidence.Addresses) != 0 {
		return Evidence{}, malformed
	}
	for _, address := range evidence.Addresses {
		if !permanentAddress.MatchString(address) {
			return Evidence{}, malformed
		}
	}
	return evidence, nil
}

func presence(data []byte, digest string) (Evidence, error) {
	evidence, err := decodeEvidence(data)
	if err != nil {
		return Evidence{}, err
	}
	if evidence.Request != digest || evidence.Absent || !evidence.Postcondition {
		return Evidence{}, refusal("lifecycle.state", "the machine hardware evidence proves no complete read of its host's permanent hardware addresses", "")
	}
	return evidence, nil
}
