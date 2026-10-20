package ceph

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	retiredReason   = "retired with its hosts, which this removal proves stopped"
	survivingReason = "removing a Ceph cluster whose hosts outlive this destroy is not yet supported (B491)"
	survivingRemedy = "remove the cluster in the same destroy as every Bootwright-owned virtual host Machine it runs on, which retires it with them; removing it while a host survives waits for B491"
)

type Retirement struct {
	Action      string   `json:"action"`
	Cluster     string   `json:"cluster"`
	FSID        string   `json:"fsid"`
	RetiredWith []string `json:"retiredWith"`
}

func requestMachines(r Request) []string {
	machines := []string{}
	for _, host := range r.Hosts {
		if host.Machine != "" {
			machines = append(machines, host.Machine)
		}
	}
	slices.Sort(machines)
	return slices.Compact(machines)
}

func (c Capability) HostMachines(block reconciliation.Block) ([]string, error) {
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return requestMachines(r), nil
}

func retirementFor(r Request) Retirement {
	return Retirement{Action: r.Action, Cluster: r.Cluster, FSID: r.FSID, RetiredWith: requestMachines(r)}
}

func (c Capability) retirement(ctx context.Context, e lifecycle.Execution) (Request, json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return r, nil, err
	}
	if len(requestMachines(r)) == 0 {
		return r, nil, failure("lifecycle.state", "a Ceph block retires only with the host Machines its frozen request names", "")
	}
	evidence, err := json.Marshal(retirementFor(r))
	if err != nil {
		return r, nil, failure("lifecycle.state", "the Ceph retirement evidence cannot be encoded", "")
	}
	if _, err := EvaluateRetirement(evidence, r, true); err != nil {
		return r, nil, err
	}
	return r, evidence, nil
}

func (c Capability) retire(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	_, evidence, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

func (c Capability) observeRetirement(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	_, evidence, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func EvaluateRetirement(data []byte, r Request, retired bool) (reconciliation.EffectState, error) {
	return evaluateRetirement(data, retirementFor(r), retired)
}

func (SMBCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	r, err := DecodeSMBRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return slices.Clone(r.Machines), nil
}

func (c SMBCapability) retirement(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return nil, err
	}
	want := Retirement{Action: "smb", Cluster: r.Cluster, FSID: FSIDFor(e.ApplyOperation, r.Cluster), RetiredWith: r.Machines}
	evidence, err := json.Marshal(want)
	if err != nil {
		return nil, failure("lifecycle.state", "the Ceph retirement evidence cannot be encoded", "")
	}
	if _, err := evaluateRetirement(evidence, want, true); err != nil {
		return nil, err
	}
	return evidence, nil
}

func (c SMBCapability) retire(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	evidence, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

func (c SMBCapability) observeRetirement(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	evidence, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func evaluateRetirement(data []byte, want Retirement, retired bool) (reconciliation.EffectState, error) {
	if !retired {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	e, err := reconciliation.DecodeEvidence[Retirement](data, 60*1024, "Ceph retirement")
	if err != nil {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	fields, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, 60*1024, "Ceph retirement members")
	if err != nil || len(fields) != 4 || canonicaljson.ProveObject(data) != nil {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	for _, key := range []string{"action", "cluster", "fsid", "retiredWith"} {
		if v, ok := fields[key]; !ok || string(v) == "null" {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
	}
	if want.FSID == "" || len(want.RetiredWith) == 0 || e.Action != want.Action || e.Cluster != want.Cluster || e.FSID != want.FSID || !slices.Equal(e.RetiredWith, want.RetiredWith) {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	return reconciliation.EffectCompleted, nil
}
