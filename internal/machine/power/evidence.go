package power

import (
	"slices"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
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
	if !slices.Contains([]string{machine.PowerOn, machine.PowerOff}, evidence.Power) {
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
		return machine.PowerOff
	}
	return machine.PowerOn
}

func decode(data []byte) (Evidence, error) {
	return reconciliation.DecodeEvidence[Evidence](data, maxEvidenceBytes, "power")
}

// Reading is one Machine's power exactly as its controller answered. A
// controller that did not answer leaves an unknown reading rather than an
// empty value, so an absent answer is never read as a state.
type Reading struct {
	Machine string `json:"machine"`
	Power   string `json:"power"`
}

// ReadEvidence is the only result shape the reading adapter may return. It
// carries one entry per Machine the survey asked about, and nothing about what
// this context owns: a reading observes a controller, it proves no ownership.
type ReadEvidence struct {
	Machines []Reading `json:"machines"`
	Request  string    `json:"request"`
}

// validateReading accepts evidence only when it answers the exact frozen
// survey: the run named this survey, and it answered once for every Machine
// the survey asked about and for nothing else.
func validateReading(data []byte, survey ReadSurvey, digest string) (map[string]string, error) {
	evidence, err := decodeReading(data)
	if err != nil {
		return nil, err
	}
	if evidence.Request != digest {
		return nil, failure("lifecycle.state", "the power reading names another survey", "")
	}
	readings := make(map[string]string, len(survey.Targets))
	for _, reading := range evidence.Machines {
		if !slices.Contains([]string{machine.PowerOn, machine.PowerOff, machine.PowerUnknown}, reading.Power) {
			return nil, failure("lifecycle.state", "the power reading reports a state this command cannot name", "")
		}
		if _, repeated := readings[reading.Machine]; repeated {
			return nil, failure("lifecycle.state", "the power reading answers for one Machine twice", "")
		}
		readings[reading.Machine] = reading.Power
	}
	for _, target := range survey.Targets {
		if _, answered := readings[target.Object]; !answered {
			return nil, failure("lifecycle.state", "the power reading does not answer for every Machine it was asked about", "")
		}
	}
	if len(readings) != len(survey.Targets) {
		return nil, failure("lifecycle.state", "the power reading answers for a Machine it was not asked about", "")
	}
	return readings, nil
}

func decodeReading(data []byte) (ReadEvidence, error) {
	return reconciliation.DecodeEvidence[ReadEvidence](data, maxReadingBytes, "power reading")
}
