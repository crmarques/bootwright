package power

import (
	"bytes"
	"encoding/json"
	"slices"
)

const maxEvidenceBytes = 8 << 10

// Evidence is the only result shape the power adapter may return. Power and
// Previous are states the controller reported, not states the request asked
// for, so a poll that never arrived leaves an empty value rather than the
// expected one.
type Evidence struct {
	Changed       bool   `json:"changed"`
	Machine       string `json:"machine"`
	Postcondition bool   `json:"postcondition"`
	Power         string `json:"power"`
	Previous      string `json:"previous"`
	Request       string `json:"request"`
}

// validate accepts evidence only when it proves the exact frozen request
// settled: the controller answered for this Machine, and the state it reported
// is the one the verb converges to.
func validate(data []byte, request Request, digest string) (Evidence, error) {
	evidence, err := decode(data)
	if err != nil {
		return Evidence{}, err
	}
	if evidence.Request != digest {
		return Evidence{}, failure("lifecycle.state", "the power evidence names another request", "")
	}
	if evidence.Machine != request.Identity.Object {
		return Evidence{}, failure("lifecycle.state", "the power evidence names another Machine", "")
	}
	if !evidence.Postcondition {
		return Evidence{}, failure("lifecycle.unknown", "the management controller did not prove the power state this operation asked for",
			"read the machine's power state again once its controller answers")
	}
	if !slices.Contains([]string{StateOn, StateOff}, evidence.Power) {
		return Evidence{}, failure("lifecycle.unknown", "the management controller reported no usable power state",
			"read the machine's power state again once its controller answers")
	}
	if evidence.Power != expected(request.Verb) {
		return Evidence{}, failure("lifecycle.unknown", "the machine did not reach the power state this operation asked for",
			"repeat the operation, or stop it with --force when the guest does not answer a graceful request")
	}
	return evidence, nil
}

// expected is the state each verb converges to. A restart ends powered on, so
// an interrupted restart is never reported as settled.
func expected(verb string) string {
	if verb == Stop {
		return StateOff
	}
	return StateOn
}

func decode(data []byte) (Evidence, error) {
	if len(data) == 0 || len(data) > maxEvidenceBytes {
		return Evidence{}, failure("lifecycle.state", "the power adapter returned no bounded evidence", "")
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, failure("lifecycle.state", "the power adapter returned malformed evidence", "")
	}
	if decoder.More() {
		return Evidence{}, failure("lifecycle.state", "the power adapter returned trailing evidence", "")
	}
	return evidence, nil
}
