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
		requests, err := c.objectRequests(catalog, object, input.Context.Name)
		if err != nil {
			return plan, err
		}
		for _, r := range requests {
			r.Controller = input.Controller
			definition, err := c.definition(input.Verb, object, r)
			if err != nil {
				return plan, err
			}
			plan.Definitions = append(plan.Definitions, definition)
		}
		if len(requests) == 0 {
			continue
		}
		request := requests[0]
		plan.Secrets = append(plan.Secrets, request.ClusterKeyRef, request.RegistryRef)
		for _, h := range request.Hosts {
			plan.Secrets = append(plan.Secrets, h.SSHKeyRef)
			if h.HostKeyRef != "" {
				plan.Secrets = append(plan.Secrets, h.HostKeyRef)
			}
		}
		for _, r := range requests {
			if r.Gateway != nil {
				for _, b := range r.Gateway.Bindings {
					plan.Secrets = append(plan.Secrets, b.SecretRef)
				}
			}
		}
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (c Capability) objectRequests(catalog api.Catalog, object api.Object, contextName string) ([]Request, error) {
	if c.kind == api.StorageObjectGateway {
		return gatewayRequests(catalog, object, contextName)
	}
	request, err := c.request(catalog, object, contextName)
	if err != nil {
		return nil, err
	}
	requests := []Request{request}
	if c.kind == api.StorageCluster {
		if settings, needed := settingsRequest(request, object); needed {
			requests = append(requests, settings)
		}
		units, err := serviceUnitRequests(request, object)
		if err != nil {
			return nil, err
		}
		requests = append(requests, units...)
	}
	return requests, nil
}

func (c Capability) definition(verb reconciliation.Verb, object api.Object, request Request) (reconciliation.BlockDefinition, error) {
	if _, err := cephDeadline(request, "apply"); err != nil {
		return reconciliation.BlockDefinition{}, err
	}
	canonical, err := request.Canonical()
	if err != nil {
		return reconciliation.BlockDefinition{}, err
	}
	impacts := []string{"realize Ceph " + subjectOf(request, object.Name())}
	consumes := []string{}
	if verb == reconciliation.Destroy {
		impacts = []string{"remove Ceph " + subjectOf(request, object.Name())}
		consumes = []string{reconciliation.AuthorizationDataLoss}
	}
	requires, dependencies := placement(object, request)
	id := "ceph-" + request.Action + "-" + object.Name()
	if request.Service != nil {
		id = ServiceUnitBlockID(request.Cluster, *request.Service)
	}
	if c.kind == api.StorageObjectGateway && request.Action == "pool" && request.Pool != nil {
		suffix, _ := zonePoolSuffix(request.Pool.Name)
		id = GatewayPoolBlockID(object.Name(), suffix)
	}
	return reconciliation.BlockDefinition{ID: id, Description: strings.Join(impacts, ""), Stage: reconciliation.StageClusters, Requires: requires, Dependencies: dependencies, Exclusive: []string{"ceph/" + request.Cluster}, Impacts: impacts, Consumes: consumes, Kind: string(c.kind), Object: object.Name(), Implementation: Implementation, ContentDigest: ContentDigest(), Request: canonical}, nil
}

func placement(object api.Object, request Request) ([]reconciliation.ObjectRef, []string) {
	requires := []reconciliation.ObjectRef{}
	dependencies := []string{ClusterBlockID(request.Cluster), ImageBlockID(request.Cluster)}
	switch request.Action {
	case "cluster":
		dependencies = []string{ImageBlockID(request.Cluster)}
		for _, host := range request.Hosts {
			requires = append(requires, hostRequirement(host))
			dependencies = append(dependencies, HostBlockID(request.Cluster, host.Machine))
		}
		slices.SortFunc(requires, func(a, b reconciliation.ObjectRef) int {
			return strings.Compare(a.Kind+"/"+a.Object, b.Kind+"/"+b.Object)
		})
		requires = slices.Compact(requires)
	case "policy":
		dependencies = []string{ClusterBlockID(request.Cluster)}
	case "service":
		if request.Service != nil {
			dependencies = unitDependencies(request, object)
		}
	case "pool":
		if ref := object.Spec().Get("placementPolicyRef").Text(); ref != "" && object.Kind() == api.StoragePool {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePlacementPolicy), Object: ref})
		}
	case "client":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: request.Client.Pool})
	case "rbd":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: request.RBD.Pool})
		if request.RBD.DataPool != "" {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: request.RBD.DataPool})
		}
	case "filesystem":
		for _, pool := range append([]string{request.Filesystem.MetadataPool}, request.Filesystem.DataPools...) {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: pool})
		}
	case "subvolume":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageFilesystem), Object: request.Subvolume.Filesystem})
	case "gateway":
		for _, suffix := range derivedZonePools(object) {
			dependencies = append(dependencies, GatewayPoolBlockID(object.Name(), suffix))
		}
		for _, pool := range declaredPlacementPools(object) {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StoragePool), Object: pool})
		}
		if request.Gateway != nil && len(request.Gateway.Documents) > 1 && len(request.Auxiliary) != 0 {
			dependencies = append(dependencies, SettingsBlockID(request.Cluster))
		}
	}
	slices.Sort(dependencies)
	return requires, dependencies
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
		if reason, remedy, gap := undeployedDelegated(cluster); gap {
			out = append(out, lifecycle.RefusalOf(object, reason, remedy))
		} else if reason, remedy, gap := unselectedAuxiliary(cluster); gap {
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
	subject := subjectOf(r, block.Object)
	return lifecycle.Removal{Description: "remove Ceph " + subject, Impacts: []string{"remove Ceph " + subject}, Consumes: []string{reconciliation.AuthorizationDataLoss}}, nil
}

func subjectOf(r Request, object string) string {
	if r.Service != nil {
		return "service " + r.Service.name() + " of " + object
	}
	if r.Gateway != nil {
		return "object gateway " + object
	}
	if r.Pool != nil && r.Pool.Name != object {
		return "zone pool " + r.Pool.Name + " of object gateway " + object
	}
	return r.Action + " " + object
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
	action := map[api.Kind]string{api.StorageCluster: "cluster", api.StoragePlacementPolicy: "policy", api.StoragePool: "pool", api.StorageClient: "client", api.StorageRBDImage: "rbd", api.StorageFilesystem: "filesystem", api.StorageSubvolume: "subvolume", api.StorageObjectGateway: "pool"}[c.kind]
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
	if c.kind == api.StorageObjectGateway {
		object = gatewayPoolObject(e.Block, r)
		if r.Action == "gateway" && r.Gateway != nil && e.Block.ID == GatewayBlockID(r.Gateway.Name) {
			action, object = r.Action, r.Gateway.Name
		}
	}
	if c.kind == api.StorageCluster && (r.Action == "settings" || r.Action == "service") {
		action = r.Action
	}
	if action == "" || r.Action != action || (r.Action == "settings") != (r.Settings != nil) || (r.Action == "service") != (r.Service != nil) || (r.Action == "gateway") != (r.Gateway != nil) || e.Block.Kind != string(c.kind) || e.Block.Object != object {
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
	if r.Auxiliary, err = ProvedAuxiliaryImages(proofs, r.Cluster, r.Auxiliary); err != nil {
		return Request{}, err
	}
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
