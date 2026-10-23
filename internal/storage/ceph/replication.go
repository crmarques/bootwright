package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

const ReplicationVersion = "ceph-replication-v1"
const ReplicationImplementation = "storage-ceph-replication-v2"
const ReplicationEvidenceVersion = "ceph-replication-evidence-v1"

func ReplicationContentDigest() string {
	sum := sha256.Sum256([]byte(ReplicationImplementation + "\x00" + ReplicationVersion + "\x00" + ReplicationEvidenceVersion + "\x00" + PhysicalRetirementImplementation))
	return hex.EncodeToString(sum[:])
}
func ReplicationBlockID(name string) string { return "ceph-replication-" + name }

type ReplicationEndpoint struct {
	Cluster    string          `json:"cluster"`
	Machines   []string        `json:"machines"`
	Pool       Pool            `json:"pool"`
	PoolObject string          `json:"poolObject"`
	Scope      json.RawMessage `json:"scope,omitempty"`
}
type ReplicationRequest struct {
	Controller string              `json:"controller"`
	Images     []RBDImage          `json:"images"`
	Mode       string              `json:"mode"`
	Object     string              `json:"object"`
	Primary    ReplicationEndpoint `json:"primary"`
	Secondary  ReplicationEndpoint `json:"secondary"`
	Version    string              `json:"version"`
}

func (r ReplicationRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "Ceph replication")
}
func DecodeReplicationRequest(data []byte) (ReplicationRequest, error) {
	r, err := reconciliation.ThawVersion[ReplicationRequest](data, "Ceph replication", ReplicationVersion)
	if err != nil {
		return r, err
	}
	if r.Controller == "" || !reconciliation.ValidSegment(ReplicationBlockID(r.Object)) || r.Primary.Cluster == "" || r.Secondary.Cluster == "" || r.Primary.Cluster == r.Secondary.Cluster || r.Mode != "image" && r.Mode != "pool" || len(r.Images) < 1 || len(r.Images) > 3 {
		return r, failure("lifecycle.state", "the frozen Ceph replication request is inconsistent", "")
	}
	for _, end := range []ReplicationEndpoint{r.Primary, r.Secondary} {
		if len(end.Scope) != 0 || !strictlySorted(end.Machines) || end.Pool.Name != r.Primary.Pool.Name || end.Pool.Type != "replicated" || end.Pool.Role != "rbd" || end.PoolObject == "" || end.Pool.Object != "" && end.Pool.Object != end.PoolObject || end.Pool.Object == "" && end.Pool.Name != end.PoolObject {
			return r, failure("lifecycle.state", "the frozen replication endpoint is inconsistent", "")
		}
	}
	for _, machine := range r.Primary.Machines {
		if slices.Contains(r.Secondary.Machines, machine) {
			return r, failure("lifecycle.state", "replication endpoints must use disjoint Machines", "")
		}
	}
	for i, image := range r.Images {
		if image.Pool != r.Primary.Pool.Name || image.DataPool != "" || !slices.Contains(image.Features, "journaling") || image.SizeBytes < 1 || image.Name == "" || i > 0 && r.Images[i-1].Name >= image.Name {
			return r, failure("lifecycle.state", "the frozen replicated image is inconsistent", "")
		}
	}
	return r, nil
}

type ReplicationCapability struct {
	runner Runner
	proofs DependencyProofs
}

func NewReplication(runner Runner) ReplicationCapability {
	return ReplicationCapability{runner: runner}
}
func (c ReplicationCapability) WithProofs(proofs DependencyProofs) ReplicationCapability {
	c.proofs = proofs
	return c
}
func (c ReplicationCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, failure("lifecycle.state", "replication planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.StorageReplication) {
		if !reconciliation.ValidSegment(ReplicationBlockID(object.Name())) {
			return plan, failure("api.invariant", "StorageReplication name does not fit its 63-character block identity", "shorten metadata.name to at most 46 characters")
		}
		if issues := storage.Validate(object, catalog); len(issues) != 0 {
			return plan, failure("api.invariant", issues[0].Message, issues[0].Remediation)
		}
		r := ReplicationRequest{Version: ReplicationVersion, Controller: input.Controller, Object: object.Name(), Images: []RBDImage{}}
		requirements := []reconciliation.ObjectRef{}
		dependencies := []string{}
		exclusive := []string{}
		for i, fields := range [][2]string{{"clusterRef", "poolRef"}, {"peerClusterRef", "peerPoolRef"}} {
			pool, _ := catalog.Find(api.StoragePool, object.Spec().Get(fields[1]).Text())
			base, err := poolRequest(catalog, pool, input.Context.Name)
			if err != nil {
				return plan, err
			}
			endpoint := ReplicationEndpoint{Cluster: base.Cluster, Machines: requestMachines(base), Pool: *base.Pool, PoolObject: pool.Name()}
			if i == 0 {
				r.Primary = endpoint
				r.Mode = pool.Spec().Get("mirroring", "mode").Text()
			} else {
				r.Secondary = endpoint
			}
			requirements = append(requirements, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: pool.Name()})
			dependencies = append(dependencies, ClusterBlockID(base.Cluster), ImageBlockID(base.Cluster))
			exclusive = append(exclusive, "ceph/"+base.Cluster)
			for _, host := range base.Hosts {
				plan.Secrets = append(plan.Secrets, host.SSHKeyRef)
				if host.HostKeyRef != "" {
					plan.Secrets = append(plan.Secrets, host.HostKeyRef)
				}
			}
		}
		for _, image := range storage.ReplicationImages(catalog, object) {
			request, err := rbdImageRequest(catalog, image, input.Context.Name)
			if err != nil {
				return plan, err
			}
			r.Images = append(r.Images, *request.RBD)
			requirements = append(requirements, reconciliation.ObjectRef{Kind: string(api.StorageRBDImage), Object: image.Name()})
		}
		dependencies = append(dependencies, ServiceUnitBlockID(r.Secondary.Cluster, ServiceUnit{Type: "rbd-mirror"}))
		slices.Sort(dependencies)
		slices.Sort(exclusive)
		canonical, err := r.Canonical()
		if err != nil {
			return plan, err
		}
		if _, err = DecodeReplicationRequest(canonical); err != nil {
			return plan, err
		}
		removal := replicationRemoval(input.Verb, r)
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: ReplicationBlockID(r.Object), Kind: string(api.StorageReplication), Object: r.Object, Implementation: ReplicationImplementation, ContentDigest: ReplicationContentDigest(), Request: canonical, Stage: reconciliation.StageClusters, Requires: requirements, Dependencies: dependencies, Exclusive: exclusive, Description: removal.Description, Impacts: removal.Impacts, Consumes: removal.Consumes})
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}
func replicationRemoval(verb reconciliation.Verb, r ReplicationRequest) lifecycle.Removal {
	action := "realize"
	consumes := []string{}
	if verb == reconciliation.Destroy {
		action = "remove"
		consumes = append(consumes, reconciliation.AuthorizationDataLoss)
	}
	description := action + " RBD replication " + r.Object + " from " + r.Primary.Cluster + " to " + r.Secondary.Cluster
	return lifecycle.Removal{Description: description, Impacts: []string{description}, Consumes: consumes}
}
func (ReplicationCapability) Removal(ctx context.Context, b reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeReplicationRequest(b.Request)
	return replicationRemoval(reconciliation.Destroy, r), err
}
func (ReplicationCapability) RequiresOnlineRemoval(reconciliation.Block) bool { return true }
func (ReplicationCapability) identity(ctx context.Context, e lifecycle.Execution) (ReplicationRequest, error) {
	if err := ctx.Err(); err != nil {
		return ReplicationRequest{}, err
	}
	r, err := DecodeReplicationRequest(e.Block.Request)
	if err != nil {
		return r, err
	}
	if e.Block.Kind != string(api.StorageReplication) || e.Block.Object != r.Object || e.Block.ID != ReplicationBlockID(r.Object) || e.ApplyOperation == "" {
		return r, failure("lifecycle.state", "replication requires its frozen resource and original apply identity", "")
	}
	return r, nil
}
func (c ReplicationCapability) prepare(ctx context.Context, e lifecycle.Execution) (ReplicationRequest, []ClusterScope, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return r, nil, err
	}
	scopes := []ClusterScope{}
	for _, end := range []*ReplicationEndpoint{&r.Primary, &r.Secondary} {
		proof, err := ProvedCluster(provedFor(e), end.Cluster, e.ApplyOperation)
		if err != nil {
			return r, nil, err
		}
		if !slices.Equal(proof.Machines, end.Machines) {
			return r, nil, failure("lifecycle.state", "replication machines do not match original cluster creation evidence", "")
		}
		if err := bindPhysicalHosts(proof.Scope.Hosts, e, c.proofs); err != nil {
			return r, nil, err
		}
		end.Scope, err = reconciliation.Freeze(proof.Scope, "Ceph replication scope")
		if err != nil {
			return r, nil, err
		}
		scopes = append(scopes, proof.Scope)
	}
	return r, scopes, nil
}
func replicationMaterial(e lifecycle.Execution, scopes []ClusterScope, readers DependencyProofs) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	bound := maps.Clone(e.Material)
	if bound == nil {
		bound = map[string]secrets.Material{}
	}
	files := []lifecycle.MaterialFile{}
	known := strings.Builder{}
	for i, scope := range scopes {
		prefix := []string{"primary", "secondary"}[i]
		bootstrap := slices.IndexFunc(scope.Hosts, func(host Host) bool { return host.Name == scope.Bootstrap })
		if bootstrap < 0 {
			return e, nil, func() {}, failure("lifecycle.state", "replication bootstrap host is absent from its proved cluster scope", "")
		}
		files = append(files, lifecycle.MaterialFile{Name: prefix + "-key", Secret: scope.Hosts[bootstrap].SSHKeyRef, Part: secrets.PrivateKeyPart, Variable: prefix + "SSHKey"})
		for _, h := range scope.Hosts {
			key, err := provedHostKey(h, e, readers)
			if err != nil {
				return e, nil, func() {}, err
			}
			known.WriteString(key.Line(h.Address, h.Port))
			known.WriteByte('\n')
		}
	}
	bound[knownHostsMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(known.String())})
	e.Material = bound
	files = append(files, lifecycle.MaterialFile{Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"})
	return e, files, func() { bound[knownHostsMaterial].Clear() }, nil
}
func replicationDeadline(r ReplicationRequest, operation string) time.Duration {
	calls := 2 + 2*len(r.Images)
	wait := 0
	if operation == "apply" {
		calls = 8 + 3*len(r.Images)
		wait = 1860
	}
	return time.Duration(calls*nativeCall+wait+60) * time.Second
}
func (c ReplicationCapability) run(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.RunResult, ReplicationRequest, error) {
	r, scopes, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, r, failure("lifecycle.state", "replication runner is not configured", "")
	}
	canonical, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	execution, files, cleanup, err := replicationMaterial(e, scopes, c.proofs)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	defer cleanup()
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: ReplicationImplementation, Operation: operation, Variable: "bootwright_ceph_replication", Canonical: canonical, Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Controller}, Materials: files, Refusals: replicationRefusals(), NativeErrors: true, Deadline: replicationDeadline(r, operation)}))
	return result, r, err
}
func (c ReplicationCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	result, r, err := c.run(ctx, e, "apply")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	effect, err := EvaluateReplicationEvidence(result.Evidence, r, e.ApplyOperation, e.Block.RequestDigest)
	if err != nil || effect != reconciliation.EffectCompleted || len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "replication did not prove both endpoints and initial synchronization", "")
	}
	outcome, known := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	if !known {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "replication returned no known outcome", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}
func (c ReplicationCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	result, r, err := c.run(ctx, e, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	if len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, failure("lifecycle.unknown", "replication returned unexpected private material", "")
	}
	effect, err := EvaluateReplicationEvidence(result.Evidence, r, e.ApplyOperation, e.Block.RequestDigest)
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, err
}
func (ReplicationCapability) HostMachines(b reconciliation.Block) ([]string, error) {
	r, err := DecodeReplicationRequest(b.Request)
	if err != nil {
		return nil, err
	}
	names := append(slices.Clone(r.Primary.Machines), r.Secondary.Machines...)
	slices.Sort(names)
	return slices.Compact(names), nil
}
func (c ReplicationCapability) retirement(ctx context.Context, e lifecycle.Execution) (json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return nil, err
	}
	if !e.RetiredWithHosts {
		return nil, failure("lifecycle.unsupported", survivingReason, survivingRemedy)
	}
	machines, err := c.HostMachines(e.Block)
	if err != nil {
		return nil, err
	}
	pending, err := pendingPhysicalClusters(e, []string{r.Primary.Cluster, r.Secondary.Cluster}, machines)
	if err != nil {
		return nil, err
	}
	return reconciliation.Freeze(struct {
		Object       string   `json:"object"`
		Owner        string   `json:"owner"`
		PendingErase []string `json:"pendingErase,omitempty"`
		Primary      string   `json:"primary"`
		RetiredWith  []string `json:"retiredWith"`
		Secondary    string   `json:"secondary"`
	}{r.Object, e.ApplyOperation, pending, FSIDFor(e.ApplyOperation, r.Primary.Cluster), machines, FSIDFor(e.ApplyOperation, r.Secondary.Cluster)}, "Ceph replication retirement")
}
func (c ReplicationCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	data, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: data}, nil
}
func (c ReplicationCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	data, err := c.retirement(ctx, e)
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: data}, nil
}
func (c ReplicationCapability) Quiescent(ctx context.Context, p lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	if p.RetiredWithHosts {
		if r, err := DecodeReplicationRequest(p.Block.Request); err == nil {
			machines, err := c.HostMachines(p.Block)
			if err != nil {
				return lifecycle.Quiescence{State: lifecycle.Unproved}, err
			}
			pending, err := pendingPhysicalClusters(p.Execution(), []string{r.Primary.Cluster, r.Secondary.Cluster}, machines)
			if err != nil {
				return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
			}
			if len(pending) != 0 {
				return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: pendingPhysicalReason}, nil
			}
			return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: retiredReason}, nil
		}
	}
	return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
}
