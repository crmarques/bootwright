package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestGatewayCompletionProvesBoundLeaf(t *testing.T) {
	for _, operation := range []string{"apply", "observe"} {
		for _, fingerprint := range []string{"matching", "wrong", "missing", "malformed"} {
			t.Run(operation+"/"+fingerprint, func(t *testing.T) {
				r := coreRequest()
				for i := range r.Hosts {
					r.Hosts[i].Devices = nil
				}
				unit := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), []api.Value{tlsBinding("mgmt-gateway", "mgmt-tls")}))
				r.Action, r.Service = "service", &unit
				e := withMaterial(executionFor(t, r), r)
				maps.Copy(e.Material, mgmtMaterial(leafPEM(t, nil, net.ParseIP("192.0.2.60"))))
				runner := &recordingRunner{}
				capability := New(api.CephCluster, runner).WithProofs(dependencyProofs{})
				prepared, err := capability.prepare(context.Background(), e)
				if err != nil {
					t.Fatal(err)
				}
				ready := readyEvidence(prepared)
				ready.Request = e.Block.RequestDigest
				ready.Daemons = []UnitDaemon{{Host: "node-1", Name: "keepalived.mgmt.node-1.abcdef", Service: "ingress.mgmt"}, {Host: "node-1", Name: "mgmt-gateway.node-1", Service: "mgmt-gateway"}}
				leaf, _ := boundLeaf(e.Material["mgmt-tls"])
				sum := sha256.Sum256(leaf.Raw)
				digest := hex.EncodeToString(sum[:])
				switch fingerprint {
				case "wrong":
					digest = strings.Repeat("a", 64)
				case "missing":
					digest = ""
				case "malformed":
					digest = "invalid"
				}
				ready.Endpoint = EndpointEvidence{CertificateSHA256: digest, Reachable: true}
				runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: evidenceBytes(t, ready)}
				good := fingerprint == "matching"
				if operation == "apply" {
					result, err := capability.Apply(context.Background(), e)
					if good && (err != nil || result.Outcome != reconciliation.OutcomeUnchanged) || !good && (err == nil || result.Outcome != reconciliation.OutcomeUnknown || len(result.Produced) != 0) {
						t.Fatalf("outcome=%v error=%v", result, err)
					}
				} else {
					result, err := capability.Observe(context.Background(), e)
					if good && (err != nil || result.Effect != reconciliation.EffectCompleted) || !good && (err == nil || result.Effect != reconciliation.EffectUnknown || len(result.Produced) != 0) {
						t.Fatalf("outcome=%v error=%v", result, err)
					}
				}
				if runner.calls != 1 {
					t.Fatalf("unexpected calls %d", runner.calls)
				}
			})
		}
	}
}

func TestGatewayBindingPreservesUnboundAndRemovalEvidence(t *testing.T) {
	r := runtimeRequest()
	unit := gatewayUnitOf(t, delegatedCatalog(vipGateway("192.0.2.60"), nil))
	r.Action, r.Service = "service", &unit
	e := executionFor(t, r)
	e.Block.RequestDigest = "digest"
	ready := readyEvidence(r)
	ready.Daemons = []UnitDaemon{{Host: "node-1", Name: "keepalived.mgmt.node-1.abcdef", Service: "ingress.mgmt"}, {Host: "node-1", Name: "mgmt-gateway.node-1", Service: "mgmt-gateway"}}
	ready.Endpoint = EndpointEvidence{CertificateSHA256: strings.Repeat("a", 64), Reachable: true}
	if effect, err := evaluateExecutionEvidence(evidenceBytes(t, ready), r, e, false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("unbound: %s %v", effect, err)
	}
	unit.Bindings = []ServiceBinding{{ServiceRef: "mgmt-gateway", Path: "/spec/ssl_cert", SecretRef: "mgmt-tls"}}
	removed := Evidence{Version: EvidenceVersion, Request: "digest", Action: r.Action, Owner: r.Owner, FSID: r.FSID, Owned: true, Removed: true, Hosts: hostNames(r), Monitors: []string{}, MGRStandbys: []string{}, MonAddrs: []MonitorAddress{}, OSDs: []OSDEvidence{}, Daemons: []UnitDaemon{}}
	if effect, err := evaluateExecutionEvidence(evidenceBytes(t, removed), r, e, true); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("removed: %s %v", effect, err)
	}
}
