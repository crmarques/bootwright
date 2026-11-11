package mirrorfill

import (
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxEvidenceBytes = 64 << 10

type FileEvidence struct {
	Path     string `json:"path"`
	Verified bool   `json:"verified"`
}

type FilesEvidence struct {
	Absent        bool           `json:"absent"`
	Files         []FileEvidence `json:"files"`
	Postcondition bool           `json:"postcondition"`
	Request       string         `json:"request"`
	Version       string         `json:"version"`
}

func decodeFilesEvidence(data []byte, digest string) (FilesEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[FilesEvidence](data, maxEvidenceBytes, "mirror files")
	if err != nil {
		return FilesEvidence{}, err
	}
	if evidence.Request != digest || evidence.Version != FilesEvidenceVersion {
		return FilesEvidence{}, refusal("lifecycle.state", "mirror files evidence names another request or version", "")
	}
	return evidence, nil
}

func foreignFile(evidence FilesEvidence, request FilesRequest) (string, bool) {
	next := 0
	for _, file := range evidence.Files {
		for next < len(request.Files) && request.Files[next].Path != file.Path {
			next++
		}
		if next == len(request.Files) {
			return file.Path, true
		}
		next++
	}
	return "", false
}

func filesEffect(evidence FilesEvidence, request FilesRequest, removal bool) reconciliation.EffectState {
	if _, foreign := foreignFile(evidence, request); foreign || evidence.Absent && len(evidence.Files) != 0 {
		return reconciliation.EffectUnknown
	}
	verified := 0
	for _, file := range evidence.Files {
		if file.Verified {
			verified++
		}
	}
	complete := verified == len(request.Files)
	switch {
	case removal && len(evidence.Files) == 0:
		return reconciliation.EffectCompleted
	case removal && complete && evidence.Postcondition:
		return reconciliation.EffectNoEffect
	case removal:
		return reconciliation.EffectPartial
	case complete && evidence.Postcondition && !evidence.Absent:
		return reconciliation.EffectCompleted
	case len(evidence.Files) == 0:
		return reconciliation.EffectNoEffect
	}
	return reconciliation.EffectPartial
}

func filesDrift(evidence FilesEvidence, request FilesRequest) error {
	if path, foreign := foreignFile(evidence, request); foreign {
		return refusal("lifecycle.state", "the artifact mirror holds "+path+", which the frozen request does not name in that place", "")
	}
	return refusal("lifecycle.state", "the artifact mirror reports its root absent beside files it holds", "")
}
