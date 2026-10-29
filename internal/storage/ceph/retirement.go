package ceph

import (
	"context"
	"encoding/json"
	"fmt"
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

const RetirementVersion = "ceph-core-retirement-v2"

type Retirement struct {
	Action       string             `json:"action"`
	Cluster      string             `json:"cluster"`
	FSID         string             `json:"fsid"`
	PendingErase bool               `json:"pendingErase,omitempty"`
	RetiredWith  []string           `json:"retiredWith"`
	Tiebreaker   *RetiredTiebreaker `json:"tiebreaker,omitempty"`
	Version      string             `json:"version,omitempty"`
}

type RetiredTiebreaker struct {
	Host    string `json:"host"`
	Machine string `json:"machine"`
	Removed bool   `json:"removed"`
}

func requestMachines(r Request) []string {
	return hostMachines(r, true)
}

func installedMachines(r Request) []string {
	return hostMachines(r, false)
}

func hostMachines(r Request, provided bool) []string {
	machines := []string{}
	for _, host := range r.Hosts {
		if host.Machine != "" && (provided || !host.Provided) {
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
	return installedMachines(r), nil
}

func retirementFor(r Request) Retirement {
	return Retirement{Action: r.Action, Cluster: r.Cluster, FSID: r.FSID, PendingErase: physicalHosts(r.Hosts), RetiredWith: requestMachines(r)}
}

func providedTiebreaker(r Request) (Host, bool) {
	if r.Stretch == nil {
		return Host{}, false
	}
	at := slices.IndexFunc(r.Hosts, func(h Host) bool { return h.Provided && h.Name == r.Stretch.Tiebreaker })
	if at < 0 {
		return Host{}, false
	}
	return r.Hosts[at], true
}

func removesTiebreaker(r Request) (Host, bool) {
	if r.Action != "cluster" {
		return Host{}, false
	}
	return providedTiebreaker(r)
}

func clusterRetirement(r Request) Retirement {
	want := retirementFor(r)
	want.Version = RetirementVersion
	if h, removes := removesTiebreaker(r); removes {
		want.Tiebreaker = &RetiredTiebreaker{Host: h.Name, Machine: h.Machine, Removed: true}
	}
	return want
}

func (c Capability) retirement(ctx context.Context, e lifecycle.Execution) (Request, json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return r, nil, err
	}
	if physicalHosts(r.Hosts) {
		if err := authorizePhysicalRetirement(e, r); err != nil {
			return r, nil, err
		}
	}
	if len(installedMachines(r)) == 0 {
		return r, nil, failure("lifecycle.state", "a Ceph block retires only with the host Machines its frozen request names", "")
	}
	evidence, err := json.Marshal(clusterRetirement(r))
	if err != nil {
		return r, nil, failure("lifecycle.state", "the Ceph retirement evidence cannot be encoded", "")
	}
	if _, err := EvaluateRetirement(evidence, r, true); err != nil {
		return r, nil, err
	}
	return r, evidence, nil
}

func (c Capability) retire(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	r, evidence, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	if _, removes := removesTiebreaker(r); removes {
		removal, err := c.tiebreaker(ctx, e, "destroy")
		if err != nil {
			return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
		}
		if !removal.Removed {
			return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "the provided tiebreaker's removal did not prove its footprint absent", "")
		}
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

func (c Capability) observeRetirement(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	r, evidence, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	if _, removes := removesTiebreaker(r); removes {
		observed, err := c.tiebreaker(ctx, e, "observe")
		if err != nil {
			return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
		}
		if !observed.Removed {
			effect := reconciliation.EffectNoEffect
			if observed.Attempted {
				effect = reconciliation.EffectPartial
			}
			return lifecycle.Observation{Effect: effect}, nil
		}
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func EvaluateRetirement(data []byte, r Request, retired bool) (reconciliation.EffectState, error) {
	return evaluateRetirement(data, clusterRetirement(r), retired)
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
	pending, err := pendingPhysicalClusters(e, []string{r.Cluster}, r.Machines)
	if err != nil {
		return nil, err
	}
	want.PendingErase = len(pending) != 0
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
	if err != nil {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	if e.Version != want.Version {
		return reconciliation.EffectUnknown, retirementVersionFailure(e.Version, want.Version)
	}
	count := 4
	for _, present := range []bool{want.PendingErase, want.Version != "", want.Tiebreaker != nil} {
		if present {
			count++
		}
	}
	if len(fields) != count || canonicaljson.ProveObject(data) != nil {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	for _, key := range []string{"action", "cluster", "fsid", "retiredWith"} {
		if v, ok := fields[key]; !ok || string(v) == "null" {
			return reconciliation.EffectUnknown, evidenceFailure()
		}
	}
	if want.FSID == "" || len(want.RetiredWith) == 0 || e.Action != want.Action || e.Cluster != want.Cluster || e.FSID != want.FSID || e.PendingErase != want.PendingErase || !slices.Equal(e.RetiredWith, want.RetiredWith) ||
		!retiredTiebreakerMatches(fields["tiebreaker"], e.Tiebreaker, want.Tiebreaker) {
		return reconciliation.EffectUnknown, evidenceFailure()
	}
	return reconciliation.EffectCompleted, nil
}

func retiredTiebreakerMatches(raw json.RawMessage, got, want *RetiredTiebreaker) bool {
	if want == nil {
		return got == nil && raw == nil
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](raw, 1024, "Ceph retired tiebreaker members")
	return err == nil && closedMembers(members, "host", "machine", "removed") && got != nil && *got == *want
}

func retirementVersionFailure(got, want string) error {
	if got == "" {
		got = "no version"
	}
	if want == "" {
		want = "no version"
	}
	return failure("lifecycle.state", fmt.Sprintf("the Ceph retirement record carries %s, and this build reads only %s", got, want),
		"continue this operation with the Bootwright build that recorded it; this build converts no older record")
}
