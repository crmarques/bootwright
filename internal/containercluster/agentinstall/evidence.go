package agentinstall

import (
	"bytes"
	"encoding/json"
)

const maxEvidenceBytes = 64 << 10

// MediaEvidence is the only result shape the boot-media adapter may return. It
// records what a later attempt needs to decide whether to build again — the
// inputs the published image was built from and the installer that built it —
// and nothing secret. The unguessable segment the image is published under is
// deliberately absent.
type MediaEvidence struct {
	Absent bool `json:"absent"`
	Image  bool `json:"image"`
	// Inputs is the request digest the published image was built from, and
	// Installer the version of the executable that built it. Together they are
	// what makes a replay able to prove that rebuilding would change nothing.
	Inputs        string `json:"inputs"`
	Installer     string `json:"installer"`
	Postcondition bool   `json:"postcondition"`
	Request       string `json:"request"`
	Work          bool   `json:"work"`
}

// ValidateMediaPresence accepts evidence only when it proves the published
// image is the image this exact request describes, built by the installer the
// declared release names.
func ValidateMediaPresence(data []byte, request MediaRequest, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the boot-media adapter did not prove its postcondition", "")
	}
	if !evidence.Image {
		return refusal("lifecycle.state", "the boot media adapter published no image", "")
	}
	if evidence.Inputs != digest {
		return refusal("lifecycle.state", "the published image was built from another request", "")
	}
	if evidence.Installer != request.Release.Version {
		return refusal("lifecycle.state", "the published image was built by another release's installer", "")
	}
	return nil
}

// ValidateMediaAbsence accepts evidence only when it positively proves the
// published image and the work area are gone.
func ValidateMediaAbsence(data []byte, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the boot-media adapter did not prove removal", "")
	}
	if evidence.Image || evidence.Work {
		return refusal("lifecycle.state", "the boot-media removal evidence still reports published content", "")
	}
	return nil
}

// ValidateMediaNoEffect accepts evidence only when it positively proves that
// nothing was published: no image and no work area.
func ValidateMediaNoEffect(data []byte, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Image || evidence.Work {
		return refusal("lifecycle.state", "the served root or the work area still carries this cluster", "")
	}
	return nil
}

// ValidateMediaPartial accepts evidence only when it positively proves this
// block is part way through: something it owns exists while the completion is
// not yet true, which the next attempt converges by building again.
func ValidateMediaPartial(data []byte, digest string) error {
	evidence, err := decodeMediaEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the boot-media evidence proves a settled state, not a partial one", "")
	}
	if !evidence.Image && !evidence.Work {
		return refusal("lifecycle.state", "the boot-media evidence reports nothing this operation published", "")
	}
	return nil
}

func decodeMediaEvidence(data []byte, digest string) (MediaEvidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media adapter returned no bounded evidence", "")
	}
	var evidence MediaEvidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media adapter returned trailing evidence", "")
	}
	if evidence.Request != digest {
		return MediaEvidence{}, refusal("lifecycle.state", "the boot-media evidence names another request", "")
	}
	return evidence, nil
}
