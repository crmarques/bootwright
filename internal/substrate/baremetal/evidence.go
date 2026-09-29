package baremetal

import (
	"bytes"
	"encoding/json"
	"slices"

	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const maxEvidenceBytes = 64 << 10

// Evidence is the only result shape this adapter may return. It records what
// the controller said the machine is, so a later operation can compare the
// machine it finds with the machine this block proved, and nothing secret.
type Evidence struct {
	Absent bool `json:"absent"`
	// Addresses is the complete hardware inventory the controller reported. An
	// inventory it could not read in full is reported as no inventory at all,
	// because a partial one cannot prove this is the declared machine.
	Addresses     []string `json:"addresses"`
	Manufacturer  string   `json:"manufacturer"`
	Model         string   `json:"model"`
	Postcondition bool     `json:"postcondition"`
	Power         string   `json:"power"`
	Request       string   `json:"request"`
	Serial        string   `json:"serial"`
	UUID          string   `json:"uuid"`
}

// ValidatePresence accepts evidence only when it proves the exact machine the
// request names answered: its controller reachable with the bound credential,
// an identity recorded, a power state read, and every declared address present
// in a complete inventory.
func ValidatePresence(data []byte, request Request, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || evidence.Absent {
		return refusal("lifecycle.state", "the machine adapter did not prove its postcondition", "")
	}
	if evidence.UUID == "" && evidence.Serial == "" {
		return refusal("lifecycle.state", "the management controller reported no identity for this machine", "")
	}
	if evidence.Power == "" {
		return refusal("lifecycle.state", "the management controller reported no power state", "")
	}
	for _, declared := range request.Addresses() {
		if !slices.Contains(evidence.Addresses, declared) {
			return refusal("lifecycle.state", "the machine does not report every hardware address this Machine declares", "")
		}
	}
	if len(request.Addresses()) == 0 {
		return refusal("lifecycle.state", "the machine request declares no hardware address to prove it by", "")
	}
	return nil
}

// ValidateAbsence accepts evidence only when the removal proved it took its
// claim back. This block created nothing on the machine, so there is nothing
// on it to observe gone: what the inverse proves is that it removed nothing,
// and the machine and its installed system are retained exactly as they were.
func ValidateAbsence(data []byte, digest string) error {
	evidence, err := decodeEvidence(data, digest)
	if err != nil {
		return err
	}
	if !evidence.Postcondition || !evidence.Absent {
		return refusal("lifecycle.state", "the machine adapter did not prove its removal", "")
	}
	if len(evidence.Addresses) != 0 || evidence.UUID != "" || evidence.Serial != "" {
		return refusal("lifecycle.state", "a removal that retains the machine reports nothing about it", "")
	}
	return nil
}

// ProvedIdentity reads the identity one presence proof recorded. It reads the
// evidence as strictly as the proof accepted it, without the request digest
// the proof was bound to, and refuses anything that is not a proof of a
// present machine with an identity.
func ProvedIdentity(data []byte) (machineref.HardwareIdentity, error) {
	evidence, err := decodeStrict(data)
	if err != nil {
		return machineref.HardwareIdentity{}, err
	}
	if !evidence.Postcondition || evidence.Absent || (evidence.UUID == "" && evidence.Serial == "") {
		return machineref.HardwareIdentity{}, refusal("lifecycle.state", "the machine evidence proves no identity", "")
	}
	return machineref.HardwareIdentity{UUID: evidence.UUID, Serial: evidence.Serial}, nil
}

// PinnedIdentity is the identity the context's current apply proved for one
// Machine: the evidence of its done bare-metal block. A block resolved by
// observation recorded no evidence of its own attempt, and a Machine this
// capability never proved has no such block, so either pins nothing.
func PinnedIdentity(machine string, blocks []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error) {
	for _, block := range blocks {
		if block.Implementation != Implementation || block.Verb != reconciliation.Apply || block.State != reconciliation.BlockDone {
			continue
		}
		if len(block.Evidence) == 0 || bytes.Equal(block.Evidence, []byte("null")) {
			return machineref.HardwareIdentity{}, false, nil
		}
		identity, err := ProvedIdentity(block.Evidence)
		if err != nil {
			return machineref.HardwareIdentity{}, false, refusal("lifecycle.state",
				"the proof that pins Machine/"+machine+" cannot be read", "destroy and apply this context so the machine is proved again")
		}
		return identity, true, nil
	}
	return machineref.HardwareIdentity{}, false, nil
}

func decodeEvidence(data []byte, digest string) (Evidence, error) {
	evidence, err := decodeStrict(data)
	if err != nil {
		return Evidence{}, err
	}
	if evidence.Request != digest {
		return Evidence{}, refusal("lifecycle.state", "the machine evidence names another request", "")
	}
	return evidence, nil
}

func decodeStrict(data []byte) (Evidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return Evidence{}, refusal("lifecycle.state", "the machine adapter returned no bounded evidence", "")
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, refusal("lifecycle.state", "the machine adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return Evidence{}, refusal("lifecycle.state", "the machine adapter returned trailing evidence", "")
	}
	return evidence, nil
}
