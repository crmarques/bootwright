package ceph

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	SignOnSuspensionVersion = "ceph-signon-suspension-v1"
	signOnPrepared          = "prepared"
	signOnSuspended         = "suspended"
	maxSuspensionDaemons    = 64
	maxSuspensionEvidence   = 8192
)

var (
	suspensionDaemon     = regexp.MustCompile(`\A[a-z][a-z0-9-]*\.[A-Za-z0-9][A-Za-z0-9_.-]{0,250}\z`)
	suspensionConfigured = regexp.MustCompile(`\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z\z`)
)

type SuspensionDaemon struct {
	Configured string `json:"configured"`
	Name       string `json:"name"`
}

type SignOnSuspensionEvidence struct {
	Baseline []SuspensionDaemon `json:"baseline"`
	Changed  bool               `json:"changed"`
	Request  string             `json:"request"`
	State    string             `json:"state"`
	Version  string             `json:"version"`
}

type suspensionPreparation struct {
	Baseline []SuspensionDaemon `json:"baseline"`
}

func validBaseline(daemons []SuspensionDaemon) bool {
	if daemons == nil || len(daemons) > maxSuspensionDaemons {
		return false
	}
	for i, daemon := range daemons {
		if !suspensionDaemon.MatchString(daemon.Name) || daemon.Configured != "" && !suspensionConfigured.MatchString(daemon.Configured) ||
			i > 0 && daemons[i-1].Name >= daemon.Name {
			return false
		}
	}
	return true
}

func decodeSuspensionEvidence(data []byte, digest, state string) (SignOnSuspensionEvidence, error) {
	e, err := reconciliation.DecodeEvidence[SignOnSuspensionEvidence](data, maxSuspensionEvidence, "Ceph sign-on suspension")
	if err != nil {
		return SignOnSuspensionEvidence{}, evidenceFailure()
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, maxSuspensionEvidence, "Ceph sign-on suspension members")
	if err != nil || !closedMembers(members, "baseline", "changed", "request", "state", "version") || canonicaljson.ProveObject(data) != nil {
		return SignOnSuspensionEvidence{}, evidenceFailure()
	}
	if e.Version != SignOnSuspensionVersion || e.Request != digest || e.State != state || !validBaseline(e.Baseline) || state != signOnPrepared && (len(e.Baseline) != 0 || e.Changed && state != signOnSuspended) {
		return SignOnSuspensionEvidence{}, evidenceFailure()
	}
	return e, nil
}

func decodePreparation(data json.RawMessage) ([]SuspensionDaemon, error) {
	var prepared suspensionPreparation
	if len(data) == 0 || len(data) > maxSuspensionEvidence || canonicaljson.DecodeClosed(data, &prepared) != nil || !validBaseline(prepared.Baseline) {
		return nil, cephRecordsMissing.Fail("the context's sign-on suspension record holds no before-state this build recorded", "restore the context's records from a matching backup")
	}
	return prepared.Baseline, nil
}

func (SignOnCapability) SignOnCluster(block reconciliation.Block) (string, error) {
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil {
		return "", err
	}
	return r.Cluster, nil
}

func hostAccess(e lifecycle.Execution, r SignOnRequest) lifecycle.Execution {
	held := make(map[string]secrets.Material, len(r.HostSecretRefs))
	for _, name := range r.HostSecretRefs {
		if material, found := e.Material[name]; found {
			held[name] = material
		}
	}
	e.Material = held
	return e
}

func (c SignOnCapability) PrepareSignOnSuspension(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	r, unit, err := c.prepare(ctx, e)
	if err != nil {
		return nil, err
	}
	result, err := c.runLending(ctx, hostAccess(e, r), "suspend_prepare", "suspend_prepare", unit, nil)
	if err != nil {
		return nil, err
	}
	evidence, err := decodeSuspensionEvidence(result.Evidence, e.Block.RequestDigest, signOnPrepared)
	if err != nil {
		return nil, err
	}
	return canonicaljson.Encode(suspensionPreparation{Baseline: evidence.Baseline}, canonicaljson.Bare)
}

func (c SignOnCapability) SuspendSignOn(ctx context.Context, e lifecycle.Execution, preparation json.RawMessage) (lifecycle.SignOnProof, error) {
	r, unit, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.SignOnProof{}, err
	}
	if _, err := decodePreparation(preparation); err != nil {
		return lifecycle.SignOnProof{}, err
	}
	result, err := c.runLending(ctx, hostAccess(e, r), "suspend", "suspend", unit, map[string]string{"signOnBaseline": string(preparation)})
	if err != nil {
		return lifecycle.SignOnProof{}, err
	}
	evidence, err := decodeSuspensionEvidence(result.Evidence, e.Block.RequestDigest, signOnSuspended)
	if err != nil {
		return lifecycle.SignOnProof{}, err
	}
	return lifecycle.SignOnProof{Changed: evidence.Changed, Provider: r.Provider.Name, LocalLogins: localLogins(provedFor(e), r)}, nil
}

func (c SignOnCapability) ResumeSignOn(ctx context.Context, e lifecycle.Execution) (lifecycle.SignOnProof, error) {
	r, unit, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.SignOnProof{}, err
	}
	result, err := c.runLending(ctx, e, "resume", "apply", unit, nil)
	if err != nil {
		if refusedBeforeEffect(err) {
			return lifecycle.SignOnProof{}, errors.Join(err, lifecycle.ErrSignOnUnchanged)
		}
		return lifecycle.SignOnProof{}, err
	}
	effect, err := EvaluateEvidence(result.Evidence, unit, e.Block.RequestDigest, false)
	if err != nil || effect != reconciliation.EffectCompleted || len(result.Produced) != 0 || !result.Outcome.Settled() {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.SignOnProof{}, cephUnproved.Fail("the resumed Ceph sign-on did not prove the requested postcondition", "repeat bootwright cluster sign-on resume --context <context> --name "+r.Cluster)
	}
	return lifecycle.SignOnProof{Changed: result.Outcome == reconciliation.OutcomeChanged, Provider: r.Provider.Name}, nil
}

func (c SignOnCapability) observeSuspended(ctx context.Context, e lifecycle.Execution, unit Request) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	r, err := DecodeSignOnRequest(e.Block.Request)
	if err != nil {
		return unknown, err
	}
	if e.SignOn != signOnSuspended {
		return unknown, cephNotReady.Fail("an interrupted sign-on suspend or resume left CephSignOn "+r.Object+" "+e.SignOn, "repeat the bootwright cluster sign-on command that was interrupted")
	}
	result, err := c.runLending(ctx, hostAccess(e, r), "observe", "observe", unit, map[string]string{"signOn": signOnSuspended})
	if err != nil {
		return unknown, err
	}
	if len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return unknown, cephResultInvalid.Fail("Ceph sign-on returned a value it never produces", "")
	}
	if _, err := decodeSuspensionEvidence(result.Evidence, e.Block.RequestDigest, signOnSuspended); err != nil {
		return unknown, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: result.Evidence}, nil
}

func refusedBeforeEffect(err error) bool {
	reported, unreachable := diagnostics.Of(err), diagnostics.Of(signOnReasons()["sign-on-provider-unreachable"])
	return len(reported) != 0 && len(unreachable) == 1 && reported[0].Code == unreachable[0].Code && strings.HasPrefix(reported[0].Message, unreachable[0].Message)
}

func localLogins(proofs []lifecycle.BlockEvidence, r SignOnRequest) []string {
	logins := []string{"dashboard"}
	id := ServiceUnitBlockID(r.Cluster, ServiceUnit{Type: "mgmt-gateway"})
	for _, proof := range proofs {
		if proof.Block.ID != id {
			continue
		}
		if gateway, err := DecodeRequest(proof.Block.Request); err == nil {
			services := credentialServices(gateway)
			for _, service := range []string{"prometheus", "alertmanager"} {
				if slices.Contains(services, service) {
					logins = append(logins, service)
				}
			}
		}
	}
	if r.Gateway.Grafana {
		logins = append(logins, "grafana")
	}
	return logins
}
