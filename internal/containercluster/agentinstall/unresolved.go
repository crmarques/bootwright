package agentinstall

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Unresolved says why an observation of this cluster's agent media proved
// nothing, from the evidence it recorded: evidence this request's own that
// none of the verb's checks accept is named by the check that decided. Empty
// evidence, evidence for another request and evidence that does not decode are
// left to the engine's general reason.
func (MediaCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeMediaRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	if _, err := decodeMediaEvidence(evidence, block.RequestDigest); err != nil {
		return lifecycle.Unresolved{}, false
	}
	effect, refused := mediaEffect(verb, evidence, request, block.RequestDigest)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, "the agent media of OpenShiftCluster "+block.Object, lifecycle.PlacedOn(request.Placement), refused)
}

// Unresolved says why an observation of this cluster's installation proved
// nothing, from the evidence it recorded: evidence this request's own that
// none of the verb's checks accept is named by the check that decided. Empty
// evidence, evidence for another request and evidence that does not decode are
// left to the engine's general reason.
func (InstallCapability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, evidence json.RawMessage) (lifecycle.Unresolved, bool) {
	request, err := DecodeInstallRequest(block.Request)
	if err != nil || len(evidence) == 0 {
		return lifecycle.Unresolved{}, false
	}
	decoded, err := decodeInstallEvidence(evidence, block.RequestDigest)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	subject := "the installation of OpenShiftCluster " + block.Object
	if verb == reconciliation.Apply && decoded.Cluster == "" && len(decoded.Powered) == 0 && len(decoded.Media) == 0 && (decoded.Inserted || !decoded.Booted) {
		return bootedSilence(subject+" on "+lifecycle.PlacedOn(request.Placement), request), true
	}
	effect, refused := installEffect(verb, evidence, request, block.RequestDigest, false)
	if effect != reconciliation.EffectUnknown {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Drifted(verb, subject, lifecycle.PlacedOn(request.Placement), refused)
}

func bootedSilence(target string, request InstallRequest) lifecycle.Unresolved {
	machines := request.Machines()
	slices.Sort(machines)
	machines = slices.Compact(machines)
	return lifecycle.Unresolved{
		Reason: target + " recorded a boot of its nodes from its image, and now nothing answers, no node runs and no node presents media, so the cluster may be installed on nodes that are off",
		Remedy: "run bootwright destroy --context " + request.Identity.Context + " before installing it again, or, if its nodes finished installing, start them with bootwright machine start --context " +
			request.Identity.Context + " --name " + strings.Join(machines, ",") + " so the installed cluster answers",
	}
}
