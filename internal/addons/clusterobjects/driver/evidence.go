package driver

import (
	"encoding/json"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type Evidence struct {
	co.Evidence
	Absent         bool   `json:"absent"`
	Classification string `json:"classification"`
	Postcondition  bool   `json:"postcondition"`
	Request        string `json:"request"`
	Retired        bool   `json:"retired,omitempty"`
	Version        string `json:"version"`
}

func DecodeEvidence(data []byte, digest, version string) (Evidence, error) {
	e, err := reconciliation.DecodeEvidence[Evidence](data, 64<<10, "add-on instance")
	if err != nil {
		return Evidence{}, err
	}
	if e.Version != version || e.Request != digest {
		return Evidence{}, failure("add-on evidence names another request or version", "")
	}
	if err := co.ValidateEvidence(e.Evidence); err != nil {
		return Evidence{}, failure("cluster object evidence contains unsafe native values", "")
	}
	return e, nil
}
func Effect(data []byte, program co.Program, digest, version string) (reconciliation.EffectState, error) {
	e, err := DecodeEvidence(data, digest, version)
	if err != nil {
		return reconciliation.EffectUnknown, err
	}
	if e.Retired {
		return reconciliation.EffectUnknown, nil
	}
	switch co.EvaluateEvidence(program, e.Evidence) {
	case co.Completed:
		if e.Postcondition {
			return reconciliation.EffectCompleted, nil
		}
		return reconciliation.EffectPartial, nil
	case co.NoEffect:
		return reconciliation.EffectNoEffect, nil
	case co.Partial:
		return reconciliation.EffectPartial, nil
	default:
		return reconciliation.EffectUnknown, nil
	}
}
func Retirement(digest, version string) (json.RawMessage, error) {
	return co.Canonical(Evidence{Version: version, Request: digest, Postcondition: true, Retired: true, Classification: "retired"})
}

func SafeEvidence(data []byte, digest, version string) json.RawMessage {
	if _, err := DecodeEvidence(data, digest, version); err != nil {
		return nil
	}
	return append(json.RawMessage(nil), data...)
}
