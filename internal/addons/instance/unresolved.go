package instance

import (
	"encoding/json"
	"strings"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	destroyAndApply = "destroy and apply again"
	removeUnowned   = "remove or correct the object on the cluster if no add-on of this context owns it"
	unprovedChain   = "no Bootwright command changes this; the apply's records prove the pinned CSV's installation and the installed CSV reports Succeeded, so leave the Subscription as it is and let the owner of the upgrade, the GitOps source, verify that the operator reached the installed CSV"
)

func (c Capability) Unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage) (lifecycle.Unresolved, bool) {
	return c.unresolved(verb, block, data, func(co.Ref) string { return destroyAndApply })
}

func (c Capability) UnresolvedAdded(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage, lineage []lifecycle.OwnedBlock) (lifecycle.Unresolved, bool) {
	cluster := ""
	if request, err := DecodeRequest(block.Request); err == nil {
		cluster = request.Cluster
	}
	return c.unresolved(verb, block, data, func(ref co.Ref) string {
		owners := declaredBy(ref, cluster, block.ID, lineage)
		if len(owners) == 0 {
			return removeUnowned
		}
		return "leave the object: " + strings.Join(owners, ", ") + " of this lineage declare it, so correct the input of that add-on instead"
	})
}

func declaredBy(ref co.Ref, cluster, own string, lineage []lifecycle.OwnedBlock) []string {
	var owners []string
	for _, held := range lineage {
		request, err := DecodeRequest(held.Block.Request)
		if held.Block.ID == own || err != nil || request.Cluster != cluster {
			continue
		}
		program, err := request.Objects()
		if err != nil {
			continue
		}
		for _, step := range program.Steps {
			if declares(step, ref) {
				owners = append(owners, "the block "+held.Block.ID+" of the operation "+held.Operation)
				break
			}
		}
	}
	return owners
}

func declares(step co.Step, ref co.Ref) bool {
	same := func(other co.Ref) bool {
		return other.Kind == ref.Kind && other.Namespace == ref.Namespace && other.Name == ref.Name
	}
	switch {
	case step.Create != nil:
		return same(step.Create.Ref)
	case step.Subscribe != nil:
		return same(step.Subscribe.Object.Ref)
	case step.CatalogReady != nil:
		return same(*step.CatalogReady)
	}
	return false
}

func (c Capability) unresolved(verb reconciliation.Verb, block reconciliation.Block, data json.RawMessage, remedy func(co.Ref) string) (lifecycle.Unresolved, bool) {
	request, err := DecodeRequest(block.Request)
	if err != nil || verb == reconciliation.Destroy {
		return lifecycle.Unresolved{}, false
	}
	evidence, err := decodeEvidence(data, block.RequestDigest)
	if err != nil {
		return lifecycle.Unresolved{}, false
	}
	for _, object := range evidence.Observations {
		if object.Ownership == co.Foreign || object.Ownership == co.Unknown {
			return lifecycle.Unresolved{Reason: object.Kind + "/" + object.Namespace + "/" + object.Name + " is foreign or could not be read", Remedy: remedy(object.Ref)}, true
		}
	}
	var gaps []co.RetentionGap
	other := true
	if program, err := request.Objects(); err == nil {
		gaps, other = co.RetentionGaps(program, evidence.Evidence)
	}
	gapped, gapReason := map[string]bool{}, ""
	for _, gap := range gaps {
		gapped[gap.Namespace+"/"+gap.Subscription] = true
		if gapReason != "" {
			gapReason += "; "
		}
		gapReason += "Subscription/" + gap.Namespace + "/" + gap.Subscription + " reports CSV " + gap.Installed + " in place of the pinned CSV " + gap.Expected + ", and its upgrade chain cannot be proved: " + gap.Detail()
	}
	if len(gaps) > 0 && !other {
		return lifecycle.Unresolved{Reason: gapReason, Remedy: unprovedChain}, true
	}
	also := ""
	if gapReason != "" {
		also = "; also " + gapReason
	}
	for _, olm := range evidence.OLM {
		if gapped[olm.Namespace+"/"+olm.Subscription] {
			continue
		}
		subscription := co.Ref{Kind: "Subscription", Namespace: olm.Namespace, Name: olm.Subscription}
		return lifecycle.Unresolved{Reason: "Subscription/" + olm.Namespace + "/" + olm.Subscription + " reports CSV " + olm.InstalledCSV + " without proving the declared installation" + also, Remedy: remedy(subscription)}, true
	}
	return lifecycle.Unresolved{Reason: "add-on " + request.Package + " on " + request.Cluster + " did not prove its objects and checks" + also, Remedy: remedy(co.Ref{})}, true
}
