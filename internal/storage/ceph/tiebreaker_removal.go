package ceph

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const TiebreakerEvidenceVersion = "ceph-tiebreaker-evidence-v1"

const tiebreakerRetirement = "tiebreaker"

type TiebreakerEvidence struct {
	Attempted bool   `json:"attempted"`
	FSID      string `json:"fsid"`
	Host      string `json:"host"`
	Owner     string `json:"owner"`
	Removed   bool   `json:"removed"`
	Request   string `json:"request"`
	Version   string `json:"version"`
}

func (c Capability) tiebreaker(ctx context.Context, e lifecycle.Execution, operation string) (TiebreakerEvidence, error) {
	r, err := c.prepare(ctx, e)
	if err != nil {
		return TiebreakerEvidence{}, err
	}
	host, removes := removesTiebreaker(r)
	if !removes {
		return TiebreakerEvidence{}, failure("lifecycle.state", "only a stretch cluster block removes a provided tiebreaker", "")
	}
	refusals := map[string]error{"tiebreaker-foreign": tiebreakerForeign(host, r.FSID), "tiebreaker-unreachable": tiebreakerUnreachable(host), "transport": tiebreakerUnreachable(host)}
	result, err := c.invoke(ctx, e, operation, r, map[string]string{"retirement": tiebreakerRetirement}, refusals)
	if err != nil {
		return TiebreakerEvidence{}, err
	}
	evidence, err := decodeTiebreakerEvidence(result.Evidence, r, host, e.Block.RequestDigest)
	if err != nil {
		return TiebreakerEvidence{}, err
	}
	if operation == "destroy" && (result.Outcome != "changed" && result.Outcome != "unchanged" || !evidence.Removed || !evidence.Attempted) {
		return TiebreakerEvidence{}, failure("lifecycle.unknown", "the provided tiebreaker Machine/"+host.Machine+"'s removal did not prove its footprint absent", "")
	}
	return evidence, nil
}

func decodeTiebreakerEvidence(data []byte, r Request, host Host, digest string) (TiebreakerEvidence, error) {
	e, err := reconciliation.DecodeEvidence[TiebreakerEvidence](data, 4096, "Ceph tiebreaker removal")
	if err != nil {
		return TiebreakerEvidence{}, evidenceFailure()
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, 4096, "Ceph tiebreaker removal members")
	if err != nil || !closedMembers(members, "attempted", "fsid", "host", "owner", "removed", "request", "version") || canonicaljson.ProveObject(data) != nil {
		return TiebreakerEvidence{}, evidenceFailure()
	}
	if e.Version != TiebreakerEvidenceVersion || e.Request != digest || e.Owner != r.Owner || e.FSID != r.FSID || e.Host != host.Name {
		return TiebreakerEvidence{}, evidenceFailure()
	}
	return e, nil
}

func tiebreakerForeign(h Host, fsid string) error {
	return failure("lifecycle.unknown", "the provided stretch tiebreaker Machine/"+h.Machine+" does not prove that it holds only this cluster's footprint, so this attempt changed nothing on it: its owner marker under /var/lib/bootwright/ceph/"+fsid+
		" is missing or names another cluster, or it holds another FSID's directory under /var/lib/ceph, a daemon other than its own MON and crash collector, or a cluster-key line or registry credential other than the ones this cluster recorded",
		"inspect Machine/"+h.Machine+" as the native error names; remove what another tool placed there outside Bootwright, then destroy again; Bootwright never removes what it cannot prove its own, and a tiebreaker decommissioned or reinstalled since the cluster was created has no exit yet (B938)")
}

func tiebreakerUnreachable(h Host) error {
	return failure("lifecycle.unknown", "the provided stretch tiebreaker Machine/"+h.Machine+" at "+machine.SSHHost(h.Address, h.Port)+" did not answer over the fleet connection, so whether this cluster's footprint remains on it is unknown and the cluster is not retired",
		"check that Machine/"+h.Machine+" answers SSH, offers the host key Secret "+h.KnownHostsRef+" pins and accepts the fleet key Secret "+h.SSHKeyRef+" for user "+h.User+
			", then destroy again; an unreachable tiebreaker is never taken as cleaned, and one decommissioned or reinstalled since the cluster was created has no exit yet (B938)")
}
