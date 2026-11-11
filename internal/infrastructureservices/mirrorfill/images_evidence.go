package mirrorfill

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type ImageEvidence struct {
	Digest     string `json:"digest"`
	Repository string `json:"repository"`
	Verified   bool   `json:"verified"`
}

type ImagesEvidence struct {
	Absent        bool            `json:"absent"`
	Images        []ImageEvidence `json:"images"`
	Postcondition bool            `json:"postcondition"`
	Request       string          `json:"request"`
	Version       string          `json:"version"`
}

func decodeImagesEvidence(data []byte, digest string) (ImagesEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[ImagesEvidence](data, maxEvidenceBytes, "mirror images")
	if err != nil {
		return ImagesEvidence{}, err
	}
	if evidence.Request != digest || evidence.Version != ImagesEvidenceVersion {
		return ImagesEvidence{}, refusal("lifecycle.state", "mirror images evidence names another request or version", "")
	}
	return evidence, nil
}

func foreignImage(evidence ImagesEvidence, request ImagesRequest) (string, bool) {
	next := 0
	for _, image := range evidence.Images {
		for next < len(request.Images) && (request.Images[next].Repository != image.Repository || request.Images[next].Digest != image.Digest) {
			next++
		}
		if next == len(request.Images) {
			return image.Repository + "@" + image.Digest, true
		}
		next++
	}
	return "", false
}

func imagesEffect(evidence ImagesEvidence, request ImagesRequest, removal bool) reconciliation.EffectState {
	if _, foreign := foreignImage(evidence, request); foreign || evidence.Absent && len(evidence.Images) != 0 {
		return reconciliation.EffectUnknown
	}
	verified := 0
	for _, image := range evidence.Images {
		if image.Verified {
			verified++
		}
	}
	switch {
	case removal && evidence.Absent && evidence.Postcondition:
		return reconciliation.EffectCompleted
	case removal:
		return reconciliation.EffectUnknown
	case verified == len(request.Images) && evidence.Postcondition:
		return reconciliation.EffectCompleted
	case len(evidence.Images) == 0:
		return reconciliation.EffectNoEffect
	}
	return reconciliation.EffectPartial
}

func imagesDrift(evidence ImagesEvidence, request ImagesRequest) error {
	if image, foreign := foreignImage(evidence, request); foreign {
		return refusal("lifecycle.state", "the image mirror reports "+image+", which the frozen request does not name in that place", "")
	}
	return refusal("lifecycle.state", "the image mirror reports its fill released beside images it holds", "")
}
