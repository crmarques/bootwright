package ceph

import (
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Capability owns one Ceph resource kind; all three share the native boundary.
type Capability struct {
	kind   api.Kind
	runner Runner
	proofs DependencyProofs
}

func New(kind api.Kind, runner Runner) Capability { return Capability{kind: kind, runner: runner} }

func ClusterBlockID(cluster string) string { return "ceph-cluster-" + cluster }

func (c Capability) WithProofs(proofs DependencyProofs) Capability { c.proofs = proofs; return c }

func (c Capability) request(catalog api.Catalog, object api.Object, contextName string) (Request, error) {
	switch c.kind {
	case api.StorageCluster:
		return baseRequest(catalog, object, contextName)
	case api.StoragePlacementPolicy:
		return policyRequest(catalog, object, contextName)
	case api.StoragePool:
		return poolRequest(catalog, object, contextName)
	case api.StorageClient:
		return clientRequest(catalog, object, contextName)
	case api.StorageRBDImage:
		return rbdImageRequest(catalog, object, contextName)
	case api.StorageFilesystem:
		return filesystemRequest(catalog, object, contextName)
	case api.StorageSubvolume:
		return subvolumeRequest(catalog, object, contextName)
	}
	return Request{}, failure("lifecycle.unsupported", "the requested kind has no core Ceph adapter", "")
}

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, failure("lifecycle.state", "Ceph planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(c.kind) {
		request, err := c.request(catalog, object, input.Context.Name)
		if err != nil {
			return plan, err
		}
		request.Controller = input.Controller
		if _, err := cephDeadline(request, "apply"); err != nil {
			return plan, err
		}
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		requires := []reconciliation.ObjectRef{}
		switch c.kind {
		case api.StorageCluster:
			for _, host := range request.Hosts {
				requires = append(requires, hostRequirement(host))
			}
			slices.SortFunc(requires, func(a, b reconciliation.ObjectRef) int {
				return strings.Compare(a.Kind+"/"+a.Object, b.Kind+"/"+b.Object)
			})
			requires = slices.Compact(requires)
		case api.StoragePool:
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageCluster), Object: request.Cluster})
			if ref := object.Spec().Get("placementPolicyRef").Text(); ref != "" {
				requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePlacementPolicy), Object: ref})
			}
		case api.StorageClient:
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: request.Client.Pool})
		case api.StorageRBDImage:
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: request.RBD.Pool})
			if request.RBD.DataPool != "" {
				requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: request.RBD.DataPool})
			}
		case api.StorageFilesystem:
			for _, pool := range append([]string{request.Filesystem.MetadataPool}, request.Filesystem.DataPools...) {
				requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: pool})
			}
		case api.StorageSubvolume:
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageFilesystem), Object: request.Subvolume.Filesystem})
		}
		impacts := []string{"realize Ceph " + request.Action + " " + object.Name()}
		consumes := []string{}
		if input.Verb == reconciliation.Destroy {
			impacts = []string{"remove Ceph " + request.Action + " " + object.Name()}
			consumes = []string{reconciliation.AuthorizationDataLoss}
		}
		dependencies := []string{}
		if c.kind == api.StoragePlacementPolicy {
			dependencies = append(dependencies, ClusterBlockID(request.Cluster))
		} else if c.kind != api.StoragePool {
			dependencies = append(dependencies, ImageBlockID(request.Cluster))
		}
		if c.kind == api.StorageCluster {
			for _, host := range request.Hosts {
				dependencies = append(dependencies, HostBlockID(request.Cluster, host.Machine))
			}
		}
		if c.kind == api.StorageRBDImage || c.kind == api.StorageFilesystem || c.kind == api.StorageSubvolume {
			dependencies = append(dependencies, ClusterBlockID(request.Cluster))
		}
		slices.Sort(dependencies)
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: "ceph-" + request.Action + "-" + object.Name(), Description: strings.Join(impacts, ""), Stage: reconciliation.StageClusters, Requires: requires, Dependencies: dependencies, Exclusive: []string{"ceph/" + request.Cluster}, Impacts: impacts, Consumes: consumes, Kind: string(c.kind), Object: object.Name(), Implementation: Implementation, ContentDigest: ContentDigest(), Request: canonical})
		plan.Secrets = append(plan.Secrets, request.ClusterKeyRef, request.RegistryRef)
		for _, h := range request.Hosts {
			plan.Secrets = append(plan.Secrets, h.SSHKeyRef)
			if h.HostKeyRef != "" {
				plan.Secrets = append(plan.Secrets, h.HostKeyRef)
			}
		}
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

// Unsupported runs before operation registration and before secret custody.
func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	var out []lifecycle.Refusal
	for _, object := range state.Effective().OfKind(c.kind) {
		cluster := object
		if c.kind != api.StorageCluster {
			cluster, _ = state.Effective().Find(api.StorageCluster, object.Spec().Get("clusterRef").Text())
		}
		ceph := cluster.Spec().Get("ceph")
		if !Supported(ceph.Get("release").Text(), ceph.Get("packageVersion").Text(), ceph.Get("cephadm", "ansible", "packageVersion").Text(),
			ceph.Get("image", "base").Text(), ceph.Get("image", "version").Text(), ceph.Get("cephadm", "clusterSSH", "transport").Text()) {
			out = append(out, lifecycle.RefusalOf(object, unsupportedReason, unsupportedRemedy))
		}
		if reason, remedy, gap := replicationNetworkGap(state.Effective(), cluster); gap {
			out = append(out, lifecycle.RefusalOf(object, reason, remedy))
		}
	}
	return out
}

func (c Capability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return lifecycle.Removal{Description: "remove Ceph " + r.Action + " " + block.Object, Impacts: []string{"remove Ceph " + r.Action + " " + block.Object}, Consumes: []string{reconciliation.AuthorizationDataLoss}}, nil
}
func (Capability) RequiresOnlineRemoval(reconciliation.Block) bool { return true }

func (c Capability) identity(ctx context.Context, e lifecycle.Execution) (Request, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	r, err := DecodeRequest(e.Block.Request)
	if err != nil {
		return Request{}, err
	}
	action := map[api.Kind]string{api.StorageCluster: "cluster", api.StoragePlacementPolicy: "policy", api.StoragePool: "pool", api.StorageClient: "client", api.StorageRBDImage: "rbd", api.StorageFilesystem: "filesystem", api.StorageSubvolume: "subvolume"}[c.kind]
	object := r.Cluster
	if r.Action == "policy" && r.Policy != nil {
		object = r.Policy.Name
	}
	if r.Action == "pool" && r.Pool != nil {
		object = r.Pool.Name
	}
	if r.Action == "client" && r.Client != nil {
		object = r.Client.Name
	}
	if r.Action == "rbd" && r.RBD != nil {
		object = r.RBD.Name
	}
	if r.Action == "filesystem" && r.Filesystem != nil {
		object = r.Filesystem.Name
	}
	if r.Action == "subvolume" && r.Subvolume != nil {
		object = r.Subvolume.Name
	}
	if action == "" || r.Action != action || e.Block.Kind != string(c.kind) || e.Block.Object != object {
		return Request{}, failure("lifecycle.state", "Ceph request does not match its frozen resource identity", "")
	}
	if r.Owner != "" || r.FSID != "" || e.ApplyOperation == "" {
		return Request{}, failure("lifecycle.state", "Ceph requires the immutable original apply identity", "")
	}
	r.Owner = e.ApplyOperation
	r.FSID = FSIDFor(e.ApplyOperation, r.Cluster)
	return r, nil
}

func (c Capability) prepare(ctx context.Context, e lifecycle.Execution) (Request, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return Request{}, err
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	image, err := ProvedImage(proofs, r.Cluster, r.Image)
	if err != nil {
		return Request{}, err
	}
	r.Image = image
	return r, nil
}
func (c Capability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	return c.mutate(ctx, e, "apply")
}
func (c Capability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	if e.RetiredWithHosts {
		return c.retire(ctx, e)
	}
	return c.mutate(ctx, e, "destroy")
}
func (c Capability) mutate(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.Result, error) {
	r, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	if operation == "apply" {
		if err := proveDisks(e, r, c.proofs); err != nil {
			return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
		}
	}
	result, err := c.run(ctx, e, operation, r)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	effect, err := EvaluateEvidence(result.Evidence, r, e.Block.RequestDigest, operation == "destroy")
	if err != nil || effect != reconciliation.EffectCompleted {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "Ceph did not prove the requested postcondition", "")
	}
	outcome := reconciliation.OutcomeChanged
	if result.Outcome == "unchanged" {
		outcome = reconciliation.OutcomeUnchanged
	} else if result.Outcome != "changed" {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "Ceph returned an unknown outcome", "")
	}
	if err := validateProduced(result.Produced, r, operation == "apply"); err != nil {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence, Produced: result.Produced}, nil
}
func (c Capability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	return c.observe(ctx, e, false)
}
func (c Capability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	if e.RetiredWithHosts {
		return c.observeRetirement(ctx, e)
	}
	return c.observe(ctx, e, true)
}
func (c Capability) observe(ctx context.Context, e lifecycle.Execution, removal bool) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	r, err := c.prepare(ctx, e)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, e, "observe", r)
	if err != nil {
		return unknown, err
	}
	effect, err := EvaluateEvidence(result.Evidence, r, e.Block.RequestDigest, removal)
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return unknown, err
	}
	if effect != reconciliation.EffectCompleted || removal {
		lifecycle.ClearProduced(result.Produced)
		result.Produced = nil
	} else if err := validateProduced(result.Produced, r, true); err != nil {
		lifecycle.ClearProduced(result.Produced)
		return unknown, err
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence, Produced: result.Produced}, nil
}

func (Capability) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	if probe.RetiredWithHosts {
		if r, err := DecodeRequest(probe.Block.Request); err == nil && len(requestMachines(r)) != 0 {
			return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: retiredReason}, nil
		}
	}
	return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
}
