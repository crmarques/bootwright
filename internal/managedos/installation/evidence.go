package installation

import (
	"bytes"
	"encoding/json"
)

const maxEvidenceBytes = 64 << 10

// Evidence is the only result shape the installation adapter may return. It
// records what a later consumer needs — the marker digest, the guest's host key
// and the address it answered on — and nothing secret.
type Evidence struct {
	Absent        bool   `json:"absent"`
	Address       string `json:"address"`
	HostKey       string `json:"hostKey"`
	Image         bool   `json:"image"`
	Marker        string `json:"marker"`
	Media         string `json:"media"`
	Postcondition bool   `json:"postcondition"`
	Power         string `json:"power"`
	Request       string `json:"request"`
	Tree          bool   `json:"tree"`
}

// ValidatePresence accepts evidence only when it proves the guest holds exactly
// the frozen marker, answered on its own host key at the selected address, has
// its media ejected and is running.
func ValidatePresence(data []byte, request Request, digest, marker string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the installation adapter did not prove its postcondition", "")
	}
	if evidence.Marker != marker {
		return refusal("lifecycle.state", "the guest does not hold the marker this operation froze", "")
	}
	if evidence.Address != request.Address {
		return refusal("lifecycle.state", "the installation evidence names another address", "")
	}
	if evidence.HostKey == "" {
		return refusal("lifecycle.state", "the installation captured no guest host key", "")
	}
	if evidence.Media != "" {
		return refusal("lifecycle.state", "the installation left its virtual media inserted", "")
	}
	if evidence.Power != "On" {
		return refusal("lifecycle.state", "the installed machine is not running", "")
	}
	if !evidence.Image || (request.Tree != nil && !evidence.Tree) {
		return refusal("lifecycle.state", "the installation did not publish everything its boot needs", "")
	}
	return nil
}

// ValidateAbsence accepts evidence only when it positively proves the published
// content is gone. The installed system stays with the Machine's disks, so this
// block never reports anything about the guest.
func ValidateAbsence(data []byte, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the installation adapter did not prove removal", "")
	}
	if evidence.Image || evidence.Tree {
		return refusal("lifecycle.state", "the installation removal evidence still reports published content", "")
	}
	return nil
}

// ValidateNoEffect accepts evidence only when it positively proves that nothing
// was installed: a powered-off machine holding no marker, with no content
// published. Anything else stays unknown.
func ValidateNoEffect(data []byte, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Marker != "" || evidence.Image || evidence.Tree {
		return refusal("lifecycle.state", "the machine or the served root still carries this installation", "")
	}
	if evidence.Power != "Off" {
		return refusal("lifecycle.state", "the machine is not powered off, so no effect is unproved", "")
	}
	return nil
}

// ValidatePartial accepts evidence only when it positively proves this
// installation is part way through: content it published is present on a guest
// that never installed, or the guest holds exactly the frozen marker while
// something the completion requires is not yet true. A guest that answers with
// another marker is another installation and is never converged; a powered-on
// guest with no marker may be installing right now, so neither is partial and
// both leave the effect unknown.
func ValidatePartial(data []byte, digest, marker string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the installation evidence proves a settled state, not a partial one", "")
	}
	if evidence.Marker != "" && evidence.Marker != marker {
		return refusal("lifecycle.state", "the guest holds an installation this operation did not perform", "")
	}
	if evidence.Marker == "" {
		if evidence.Power != "Off" {
			return refusal("lifecycle.state", "the machine is not powered off, so its installation is unproved", "")
		}
		if !evidence.Image && !evidence.Tree {
			return refusal("lifecycle.state", "the installation evidence reports nothing this operation published", "")
		}
	}
	return nil
}

func decodeEvidence(data []byte, digest string) (Evidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return Evidence{}, refusal("lifecycle.state", "the installation adapter returned no bounded evidence", "")
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, refusal("lifecycle.state", "the installation adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return Evidence{}, refusal("lifecycle.state", "the installation adapter returned trailing evidence", "")
	}
	if evidence.Request != digest {
		return Evidence{}, refusal("lifecycle.state", "the installation evidence names another request", "")
	}
	return evidence, nil
}
