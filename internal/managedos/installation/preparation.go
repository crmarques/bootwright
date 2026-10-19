package installation

import (
	"encoding/json"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxPreparationBytes = 16384

type registrationPreparation struct {
	Marker string `json:"marker"`
}

func namesMarker(data json.RawMessage, marker []byte) bool {
	preparation, err := reconciliation.DecodeEvidence[registrationPreparation](data, maxPreparationBytes, "installation")
	return err == nil && preparation.Marker != "" && preparation.Marker == string(marker)
}

func markerPreparation(marker []byte) func(json.RawMessage) error {
	return func(data json.RawMessage) error {
		if !namesMarker(data, marker) {
			return refusal("lifecycle.state", "the installation's automation published a before-state that does not name this installation's marker", "")
		}
		return nil
	}
}

func registeredBefore(contextName, machine string, preparations []json.RawMessage, marker []byte) (bool, error) {
	for _, data := range preparations {
		if !namesMarker(data, marker) {
			return false, refusal("lifecycle.state", "the apply records of Machine/"+machine+" hold a before-state this executable cannot read",
				"review its durable state with bootwright status --context "+contextName)
		}
	}
	return len(preparations) != 0, nil
}
