package installation

import (
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type TreeEvidence struct {
	Absent        bool   `json:"absent"`
	Content       bool   `json:"content"`
	Identity      string `json:"identity"`
	Postcondition bool   `json:"postcondition"`
	Request       string `json:"request"`
	Staging       bool   `json:"staging"`
	Tree          bool   `json:"tree"`
	Version       string `json:"version"`
}

func decodeTreeEvidence(data []byte, digest string) (TreeEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[TreeEvidence](data, maxEvidenceBytes, "package tree")
	if err != nil {
		return TreeEvidence{}, err
	}
	if evidence.Request != digest || evidence.Version != TreeEvidenceVersion {
		return TreeEvidence{}, refusal("lifecycle.state", "package tree evidence names another request or version", "")
	}
	if evidence.Identity != "" {
		normalized, ok := managedos.NormalizeMediaDigest(evidence.Identity)
		if !ok || normalized != evidence.Identity {
			return TreeEvidence{}, refusal("lifecycle.state", "package tree evidence has an invalid source identity", "")
		}
	}
	return evidence, nil
}
func treeEffect(e TreeEvidence, request TreeRequest, removal bool) reconciliation.EffectState {
	if e.Tree && (!e.Content || e.Identity != request.Media.SHA256) || e.Identity != "" && !e.Content {
		return reconciliation.EffectUnknown
	}
	left := e.Content || e.Staging || e.Tree || e.Identity != ""
	if e.Absent && left {
		return reconciliation.EffectUnknown
	}
	if removal {
		if !left {
			return reconciliation.EffectCompleted
		}
		if e.Tree && e.Content && e.Identity == request.Media.SHA256 && !e.Staging && e.Postcondition {
			return reconciliation.EffectNoEffect
		}
		return reconciliation.EffectPartial
	}
	if e.Identity != "" && e.Identity != request.Media.SHA256 {
		return reconciliation.EffectUnknown
	}
	if e.Tree && e.Content && e.Identity == request.Media.SHA256 && !e.Staging && !e.Absent && e.Postcondition {
		return reconciliation.EffectCompleted
	}
	if !left {
		return reconciliation.EffectNoEffect
	}
	return reconciliation.EffectPartial
}
