package ceph

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const ManagersEvidenceVersion = "ceph-managers-evidence-v1"

const managersRetirement = "managers"

type ManagersEvidence struct {
	FSID    string `json:"fsid"`
	Owner   string `json:"owner"`
	Request string `json:"request"`
	Stopped bool   `json:"stopped"`
	Version string `json:"version"`
}

func retiresManagers(r Request) bool {
	if r.Action != "cluster" {
		return false
	}
	physical, retained := false, false
	for _, host := range r.Hosts {
		physical = physical || host.Physical
		retained = retained || host.Physical && host.RetainRoot
	}
	_, removes := removesTiebreaker(r)
	return physical && (retained || removes)
}

func (c Capability) managers(ctx context.Context, e lifecycle.Execution, operation string) (ManagersEvidence, error) {
	r, err := c.prepare(ctx, e)
	if err != nil {
		return ManagersEvidence{}, err
	}
	if !retiresManagers(r) {
		return ManagersEvidence{}, cephRequestInvalid.Fail("only a cluster block on physical hosts whose retirement removes an FSID from a host stops its managers", "")
	}
	refusals := map[string]error{"managers-running": managersRunning(), "transport": managersUnreachable()}
	result, err := c.invoke(ctx, e, operation, r, map[string]string{"retirement": managersRetirement}, refusals)
	if err != nil {
		return ManagersEvidence{}, err
	}
	evidence, err := decodeManagersEvidence(result.Evidence, r, e.Block.RequestDigest)
	if err != nil {
		return ManagersEvidence{}, err
	}
	if operation == "destroy" && (!result.Outcome.Settled() || !evidence.Stopped) {
		return ManagersEvidence{}, managersRunning()
	}
	return evidence, nil
}

func decodeManagersEvidence(data []byte, r Request, digest string) (ManagersEvidence, error) {
	e, err := reconciliation.DecodeEvidence[ManagersEvidence](data, 4096, "Ceph managers retirement")
	if err != nil {
		return ManagersEvidence{}, evidenceFailure()
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, 4096, "Ceph managers retirement members")
	if err != nil || !closedMembers(members, "fsid", "owner", "request", "stopped", "version") || canonicaljson.ProveObject(data) != nil {
		return ManagersEvidence{}, evidenceFailure()
	}
	if e.Version != ManagersEvidenceVersion || e.Request != digest || e.Owner != r.Owner || e.FSID != r.FSID {
		return ManagersEvidence{}, evidenceFailure()
	}
	return e, nil
}

func managersRunning() error {
	return cephBoundaryUnproved.Fail("a manager of this cluster still runs or stays enabled on a physical host after the stop round, so no host's FSID removal starts",
		"inspect the attempt's adapter output, then destroy again; the stop round repeats before the tiebreaker or any host is cleaned")
}

func managersUnreachable() error {
	return cephBoundaryUnproved.Fail("a physical host of this cluster did not answer, so whether its managers still run is unknown and no host's FSID removal starts",
		"check that every physical Ceph host Machine is powered on and answers SSH with its fleet key and pinned host key, then destroy again; a host that does not answer is never taken as stopped")
}
