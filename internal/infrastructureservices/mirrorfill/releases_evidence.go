package mirrorfill

import (
	"regexp"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxReleaseEvidenceBytes = 128 << 10

type ComponentEvidence struct {
	Digest string `json:"digest"`
	Name   string `json:"name"`
}

type ReleaseEvidence struct {
	Absent        bool                `json:"absent"`
	Components    []ComponentEvidence `json:"components"`
	Digest        string              `json:"digest"`
	Missing       int                 `json:"missing"`
	Postcondition bool                `json:"postcondition"`
	Request       string              `json:"request"`
	Version       string              `json:"version"`
}

var componentNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)

func decodeReleaseEvidence(data []byte, digest string) (ReleaseEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[ReleaseEvidence](data, maxReleaseEvidenceBytes, "mirror release")
	if err != nil {
		return ReleaseEvidence{}, err
	}
	if evidence.Request != digest || evidence.Version != ReleaseEvidenceVersion {
		return ReleaseEvidence{}, refusal("lifecycle.state", "mirror release evidence names another request or version", "")
	}
	if len(evidence.Components) > MaxComponents || evidence.Missing < 0 || evidence.Missing > MaxComponents || evidence.Digest != "" && !imageDigestPattern.MatchString(evidence.Digest) {
		return ReleaseEvidence{}, refusal("lifecycle.state", "mirror release evidence names a component or digest outside its bounds", "")
	}
	for _, component := range evidence.Components {
		if !componentNamePattern.MatchString(component.Name) || len(component.Name) > 128 || !imageDigestPattern.MatchString(component.Digest) {
			return ReleaseEvidence{}, refusal("lifecycle.state", "mirror release evidence names a component or digest outside its bounds", "")
		}
	}
	return evidence, nil
}

func releaseEffect(evidence ReleaseEvidence, request ReleaseRequest, removal bool) reconciliation.EffectState {
	foreign := evidence.Digest != "" && request.Release.Digest != "" && evidence.Digest != request.Release.Digest
	if foreign || evidence.Absent && (evidence.Digest != "" || len(evidence.Components) != 0) {
		return reconciliation.EffectUnknown
	}
	switch {
	case removal && evidence.Absent && evidence.Postcondition:
		return reconciliation.EffectCompleted
	case removal:
		return reconciliation.EffectUnknown
	case evidence.Digest != "" && len(evidence.Components) != 0 && evidence.Missing == 0 && evidence.Postcondition:
		return reconciliation.EffectCompleted
	case evidence.Digest == "" && len(evidence.Components) == 0:
		return reconciliation.EffectNoEffect
	}
	return reconciliation.EffectPartial
}

func releaseDrift(evidence ReleaseEvidence, request ReleaseRequest) error {
	if evidence.Digest != "" && request.Release.Digest != "" && evidence.Digest != request.Release.Digest {
		return refusal("lifecycle.state", "the image mirror holds the release "+request.Release.Tag+" at "+evidence.Digest+", not the pinned "+request.Release.Digest, "")
	}
	return refusal("lifecycle.state", "the image mirror reports its release copy released beside a release it holds", "")
}
