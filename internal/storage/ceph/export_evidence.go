package ceph

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type ConsumerEvidence struct {
	Address    string                       `json:"address"`
	Entries    map[string]string            `json:"entries"`
	FSID       string                       `json:"fsid"`
	KeyType    string                       `json:"keyType"`
	Leader     string                       `json:"leader"`
	Name       string                       `json:"name"`
	Owner      string                       `json:"owner"`
	Principals map[string]map[string]string `json:"principals"`
	Ready      bool                         `json:"ready"`
	Request    string                       `json:"request"`
	RGWUser    string                       `json:"rgwUser"`
	Version    string                       `json:"version"`
}

func EvaluateExportEvidence(data []byte, r ConsumerRequest, owner, digest string) (reconciliation.EffectState, error) {
	e, err := reconciliation.DecodeEvidence[ConsumerEvidence](data, 16384, "Ceph consumer")
	if err != nil || e.Version != ConsumerEvidenceVersion || e.Name != r.Name || e.Owner != owner || e.FSID != FSIDFor(owner, r.Cluster) || e.Request != digest {
		return reconciliation.EffectUnknown, failure("lifecycle.state", "Ceph consumer evidence does not match its frozen identity", "")
	}
	prefix := ConsumerPrincipalPrefix(owner, r.Name)
	principals := map[string]map[string]string{}
	for suffix, caps := range r.Principals {
		principals["client."+prefix+"-"+suffix] = caps
	}
	want, _ := json.Marshal(principals)
	got, _ := json.Marshal(e.Principals)
	entries, _ := json.Marshal(exportEntries(r, owner))
	actual, _ := json.Marshal(e.Entries)
	rgw := ""
	if r.Gateway != nil {
		rgw = prefix + "-ops"
	}
	if e.KeyType != r.KeyType || r.Monitors[e.Leader] != e.Address || e.Address == "" || string(want) != string(got) || string(entries) != string(actual) || e.RGWUser != rgw {
		return reconciliation.EffectUnknown, failure("lifecycle.state", "Ceph consumer evidence does not prove its principals and endpoints", "")
	}
	if !e.Ready {
		return reconciliation.EffectPartial, nil
	}
	return reconciliation.EffectCompleted, nil
}
func ConsumerPrincipalPrefix(owner, name string) string {
	sum := sha256.Sum256([]byte("bootwright.storage-export.principal-v1\x00" + owner + "\x00" + name))
	return "bw-" + hex.EncodeToString(sum[:8])
}

type exportEntry struct {
	Data map[string]string `json:"data"`
	Kind string            `json:"kind"`
	Name string            `json:"name"`
}

func validateExportProduced(produced []lifecycle.Produced, r ConsumerRequest, owner string) error {
	fail := func() error {
		return failure("lifecycle.state", "Ceph consumer external details are incomplete or inconsistent", "")
	}
	if len(produced) != 1 || produced[0].Name != ConsumerOutput || !slices.Equal(produced[0].Material.Parts(), []secrets.Part{secrets.ValuePart}) {
		return fail()
	}
	data, ok := produced[0].Material.Part(secrets.ValuePart)
	defer clear(data)
	if !ok {
		return fail()
	}
	entries, err := reconciliation.DecodeEvidence[[]exportEntry](data, secrets.MaxPartBytes, "Ceph consumer details")
	if err != nil {
		return fail()
	}
	byName := map[string]exportEntry{}
	for _, entry := range entries {
		if _, ok := byName[entry.Name]; ok {
			return fail()
		}
		byName[entry.Name] = entry
		for _, v := range entry.Data {
			if v == "" {
				return fail()
			}
		}
	}
	expected := exportEntries(r, owner)
	prefix := ConsumerPrincipalPrefix(owner, r.Name)
	if len(expected) != len(entries) {
		return fail()
	}
	for name, kind := range expected {
		if byName[name].Kind != kind {
			return fail()
		}
	}
	if err := validateExportEntryKeys(byName); err != nil {
		return err
	}
	for name, kind := range map[string]string{"ceph-rbd": "rbd", "cephfs": "fs"} {
		for field, suffix := range map[string]string{"provisioner": "prov", "controller-expand": "prov", "node-stage": "node"} {
			if byName[name].Data["csi.storage.k8s.io/"+field+"-secret-name"] != "rook-"+prefix+"-"+kind+"-"+suffix {
				return fail()
			}
		}
	}
	if byName["rook-ceph-mon"].Data["admin-secret"] != "admin-secret" || byName["rook-ceph-mon"].Data["mon-secret"] != "mon-secret" {
		return fail()
	}
	if byName["rook-ceph-mon"].Data["fsid"] != FSIDFor(owner, r.Cluster) || byName["ceph-rbd"].Data["pool"] != r.Pool || byName["cephfs"].Data["fsName"] != r.Filesystem || byName["cephfs"].Data["pool"] != r.DefaultDataPool {
		return fail()
	}
	for _, suffix := range []string{"health", "rbd-node", "rbd-prov", "fs-node", "fs-prov"} {
		name, id := "rook-"+prefix+"-"+suffix, prefix+"-"+suffix
		if suffix == "health" {
			name, id = "rook-ceph-operator-creds", "client."+id
		}
		entry := byName[name]
		if len(entry.Data) != 2 || entry.Data["userID"] != id || !exportKey(entry.Data["userKey"], r.KeyType) {
			return fail()
		}
	}
	monitor := byName["monitoring-endpoint"].Data
	if len(monitor) != 2 || monitor["MonitoringEndpoint"] != strings.Join(r.MonitoringHosts, ",") || monitor["MonitoringPort"] != strconv.Itoa(r.MonitoringPort) {
		return fail()
	}
	mon := byName["rook-ceph-mon-endpoints"].Data
	leader, addr, ok := strings.Cut(mon["data"], "=")
	declared := r.Monitors[leader]
	if strings.Contains(declared, ":") {
		declared = "[" + declared + "]"
	}
	if !ok || declared == "" || addr != declared+":3300" || mon["maxMonId"] != "0" || mon["mapping"] != "{}" || len(mon) != 3 {
		return fail()
	}
	if byName["external-cluster-user-command"].Data["args"] != "bootwright-managed-export" {
		return fail()
	}
	return validateExportGatewayEntries(byName, r.Gateway)
}
func (c ConsumerCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	result, r, err := c.runExport(ctx, e, "apply")
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	effect, err := EvaluateExportEvidence(result.Evidence, r, e.ApplyOperation, e.Block.RequestDigest)
	if err == nil && effect == reconciliation.EffectCompleted {
		err = validateExportProduced(result.Produced, r, e.ApplyOperation)
	}
	outcome, ok := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	if err != nil || effect != reconciliation.EffectCompleted || !ok {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "Ceph consumer did not prove its complete postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence, Produced: result.Produced}, nil
}
func (c ConsumerCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	result, r, err := c.runExport(ctx, e, "observe")
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	effect, err := EvaluateExportEvidence(result.Evidence, r, e.ApplyOperation, e.Block.RequestDigest)
	if err == nil && effect == reconciliation.EffectCompleted {
		err = validateExportProduced(result.Produced, r, e.ApplyOperation)
	}
	if err != nil || effect != reconciliation.EffectCompleted {
		lifecycle.ClearProduced(result.Produced)
		result.Produced = nil
	}
	if err != nil {
		effect = reconciliation.EffectUnknown
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence, Produced: result.Produced}, err
}
func (ConsumerCapability) RequiresOnlineRemoval(reconciliation.Block) bool { return true }
func (ConsumerCapability) HostMachines(block reconciliation.Block) ([]string, error) {
	r, err := DecodeConsumerRequest(block.Request)
	return slices.Clone(r.Machines), err
}
func (c ConsumerCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeConsumerRequest(block.Request)
	return lifecycle.Removal{Description: "retire Ceph consumer " + r.Name + " with its owned hosts", Impacts: []string{"retire Ceph consumer " + r.Name + " with its owned hosts"}, Consumes: []string{reconciliation.AuthorizationDataLoss}}, err
}
func (c ConsumerCapability) retirement(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return nil, err
	}
	if !e.RetiredWithHosts || r.PhysicalHosts {
		return nil, failure("lifecycle.unsupported", survivingReason, survivingRemedy)
	}
	pending, err := pendingPhysicalClusters(e, []string{r.Cluster}, r.Machines)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Retirement{Action: "export", Cluster: r.Cluster, FSID: FSIDFor(e.ApplyOperation, r.Cluster), RetiredWith: r.Machines, PendingErase: len(pending) != 0})
}
func (c ConsumerCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	data, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: data}, nil
}
func (c ConsumerCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	data, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: data}, nil
}
func (c ConsumerCapability) Quiescent(ctx context.Context, p lifecycle.Probe) (lifecycle.Quiescence, error) {
	_, err := c.retirement(ctx, p.Execution())
	if err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: retiredReason}, nil
}

func exportKey(value, kind string) bool {
	data, err := base64.StdEncoding.Strict().DecodeString(value)
	if err != nil || base64.StdEncoding.EncodeToString(data) != value {
		return false
	}
	defer clear(data)
	size, typ := 16, uint16(1)
	if kind == "aes256k" {
		size, typ = 32, 2
	}
	return len(data) == 12+size && binary.LittleEndian.Uint16(data[:2]) == typ && int(binary.LittleEndian.Uint16(data[10:12])) == size
}

func exportEntries(r ConsumerRequest, owner string) map[string]string {
	expected := map[string]string{"external-cluster-user-command": "ConfigMap", "rook-ceph-mon-endpoints": "ConfigMap", "rook-ceph-mon": "Secret", "rook-ceph-operator-creds": "Secret", "monitoring-endpoint": "CephCluster", "ceph-rbd": "StorageClass", "cephfs": "StorageClass"}
	prefix := ConsumerPrincipalPrefix(owner, r.Name)
	for _, suffix := range []string{"rbd-node", "rbd-prov", "fs-node", "fs-prov"} {
		expected["rook-"+prefix+"-"+suffix] = "Secret"
	}
	if r.Gateway != nil {
		expected["ceph-rgw"] = "StorageClass"
		expected["rgw-admin-ops-user"] = "Secret"
		if r.Gateway.TLS {
			expected["ceph-rgw-tls-cert"] = "Secret"
		}
	}
	return expected
}

func validateExportEntryKeys(byName map[string]exportEntry) error {
	fail := func() error {
		return failure("lifecycle.state", "Ceph consumer external details are incomplete or inconsistent", "")
	}
	keySets := map[string][]string{
		"external-cluster-user-command": {"args"}, "rook-ceph-mon-endpoints": {"data", "mapping", "maxMonId"}, "rook-ceph-mon": {"admin-secret", "fsid", "mon-secret"}, "monitoring-endpoint": {"MonitoringEndpoint", "MonitoringPort"},
		"ceph-rbd": {"pool", "csi.storage.k8s.io/provisioner-secret-name", "csi.storage.k8s.io/controller-expand-secret-name", "csi.storage.k8s.io/node-stage-secret-name"},
		"cephfs":   {"fsName", "pool", "csi.storage.k8s.io/provisioner-secret-name", "csi.storage.k8s.io/controller-expand-secret-name", "csi.storage.k8s.io/node-stage-secret-name"},
		"ceph-rgw": {"endpoint", "poolPrefix"}, "rgw-admin-ops-user": {"accessKey", "secretKey"}, "ceph-rgw-tls-cert": {"cert"},
	}
	for name, entry := range byName {
		keys, ok := keySets[name]
		if !ok {
			keys = []string{"userID", "userKey"}
		}
		if len(entry.Data) != len(keys) {
			return fail()
		}
		for _, key := range keys {
			if entry.Data[key] == "" {
				return fail()
			}
		}
	}
	return nil
}

func validateExportGatewayEntries(byName map[string]exportEntry, g *ConsumerGateway) error {
	fail := func() error {
		return failure("lifecycle.state", "Ceph consumer external details are incomplete or inconsistent", "")
	}
	if g != nil {
		endpoint := fmt.Sprintf("%s:%d", g.FQDN, g.Port)
		if byName["ceph-rgw"].Data["endpoint"] != endpoint || byName["ceph-rgw"].Data["poolPrefix"] != g.Zone {
			return fail()
		}
		if g.TLS {
			chain := []byte(byName["ceph-rgw-tls-cert"].Data["cert"])
			leaf := true
			for len(strings.TrimSpace(string(chain))) > 0 {
				block, rest := pem.Decode(chain)
				if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
					return fail()
				}
				certificate, err := x509.ParseCertificate(block.Bytes)
				if err != nil || leaf && certificate.VerifyHostname(g.FQDN) != nil {
					return fail()
				}
				leaf = false
				chain = rest
			}
			if leaf {
				return fail()
			}
		}
	}
	return nil
}
