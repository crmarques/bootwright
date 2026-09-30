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
	// Private is material only the installing machine may read. A completed
	// installation has already withdrawn it, so evidence still reporting it is
	// unfinished work rather than a settled state.
	Private bool `json:"private"`
	// Reachable is the fleet account answering on the host key the machine
	// reported. An installation whose machine cannot be logged in to is not a
	// finished installation, and an observation proves it the same way an apply
	// does, so one rule governs both paths.
	Reachable bool   `json:"reachable"`
	Request   string `json:"request"`
	// Tree is the published package tree complete, its .treeinfo in place, and
	// TreeContent anything at all at the tree's published path. A removal
	// withdraws the marker before the rest of the tree, so one stopped part
	// way leaves the directory without it: content a removal still takes
	// back, though no complete tree.
	Tree        bool `json:"tree"`
	TreeContent bool `json:"treeContent"`
	// TreeStaging is anything at the path the tree is extracted at before its
	// rename: an apply killed while it extracted leaves a partial tree there,
	// beneath the served root. Work is the area the installer image is built
	// in, outside the served root, which any attempt killed part way leaves.
	TreeStaging bool `json:"treeStaging"`
	Work        bool `json:"work"`
}

// published reports whether anything this installation leaves beneath the
// served root is there.
func (e Evidence) published() bool {
	return e.Image || e.Private || e.Tree || e.TreeContent || e.TreeStaging
}

// left reports whether anything a removal takes back is there: the served
// content, and the work area, which is never served and so never proves an
// apply's effect but is still this installation's to remove.
func (e Evidence) left() bool {
	return e.published() || e.Work
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
	if !evidence.Reachable {
		return refusal("lifecycle.state", "the installed machine did not answer as its fleet account", "")
	}
	if evidence.Media != "" {
		return refusal("lifecycle.state", "the installation left its virtual media inserted", "")
	}
	if evidence.Private {
		return refusal("lifecycle.state", "the installation left material only its machine may read published", "")
	}
	if evidence.Power != "On" {
		return refusal("lifecycle.state", "the installed machine is not running", "")
	}
	if !evidence.Image || (request.Tree != nil && !evidence.Tree) {
		return refusal("lifecycle.state", "the installation did not publish everything its boot needs", "")
	}
	return nil
}

// HostKeyEvidence reports the address an installation proved and the host key
// the installed system presents there. A consumer outside the lifecycle reads
// it to pin a session against a key this context already proved, so it decodes
// without the request digest a running operation compares.
func HostKeyEvidence(data []byte) (address, hostKey string, err error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return "", "", refusal("lifecycle.state", "the installation evidence is not bounded", "")
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil || len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return "", "", refusal("lifecycle.state", "the installation evidence could not be decoded", "")
	}
	if evidence.Absent || evidence.HostKey == "" || evidence.Address == "" {
		return "", "", refusal("lifecycle.state", "the installation evidence proves no host key", "")
	}
	return evidence.Address, evidence.HostKey, nil
}

// ValidateAbsence accepts evidence only when it positively proves the published
// content and the work area are gone. The installed system stays with the
// Machine's disks, so this block never reports anything about the guest.
func ValidateAbsence(data []byte, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the installation adapter did not prove removal", "")
	}
	if evidence.left() {
		return refusal("lifecycle.state", "the installation removal evidence still reports content it takes back", "")
	}
	return nil
}

// ValidateWithdrawn accepts evidence only when it names this request and
// reports nothing left of what the removal takes back. The installed system
// stays with the Machine's disks, so neither the marker nor the power state is
// read.
func ValidateWithdrawn(data []byte, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if evidence.left() {
		return refusal("lifecycle.state", "the artifact server's host still carries this installation's content", "")
	}
	return nil
}

// ValidateWithdrawalUnfinished accepts evidence only when it names this request
// and reports something left of what the removal takes back, which it
// withdraws whatever the guest holds, so the next attempt converges it.
func ValidateWithdrawalUnfinished(data []byte, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.left() {
		return refusal("lifecycle.state", "the artifact server's host carries none of this installation's content", "")
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
	if evidence.Marker != "" || evidence.published() {
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
		if !evidence.published() {
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
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return Evidence{}, refusal("lifecycle.state", "the installation adapter returned trailing evidence", "")
	}
	if evidence.Request != digest {
		return Evidence{}, refusal("lifecycle.state", "the installation evidence names another request", "")
	}
	return evidence, nil
}
