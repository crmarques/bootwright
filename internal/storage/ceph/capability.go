package ceph

import (
	"context"
	"maps"
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

func ClusterBlockID(cluster string) string {
	return reconciliation.BlockIdentity("ceph-cluster-", cluster)
}

func (c Capability) WithProofs(proofs DependencyProofs) Capability { c.proofs = proofs; return c }

func (c Capability) request(catalog api.Catalog, object api.Object, d derivation) (Request, error) {
	switch c.kind {
	case api.CephCluster:
		return d.base(catalog, object)
	case api.CephPlacementPolicy:
		return policyRequest(catalog, object, d)
	case api.CephPool:
		return poolRequest(catalog, object, d)
	case api.CephClient:
		return clientRequest(catalog, object, d)
	case api.CephRBDImage:
		return rbdImageRequest(catalog, object, d)
	case api.CephFilesystem:
		return filesystemRequest(catalog, object, d)
	case api.CephSubvolume:
		return subvolumeRequest(catalog, object, d)
	case api.CephNVMeoFSubsystem:
		return nvmeofRequest(catalog, object, d)
	case api.CephNFSExport:
		return nfsExportRequest(catalog, object, d)
	case api.CephObjectUser:
		return objectUserRequest(catalog, object, d)
	case api.CephObjectBucket:
		return objectBucketRequest(catalog, object, d)
	}
	return Request{}, cephUnsupportedProfile.Fail("the requested kind has no core Ceph adapter", "")
}

func (c Capability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, cephRequestInvalid.Fail("Ceph planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(c.kind) {
		requests, err := c.objectRequests(catalog, object, planDerivation(input))
		if err != nil {
			return plan, err
		}
		for _, r := range requests {
			r.Controller = input.Controller
			if r.Action == "cluster" && input.Verb == reconciliation.Apply {
				if err := validateSSHSourceAdmission(catalog, r); err != nil {
					return plan, err
				}
			}
			definition, err := c.definition(input.Verb, object, r)
			if err != nil {
				return plan, err
			}
			if r.Action == "client" || r.Action == "rbd" || r.Action == "filesystem" {
				for i, ref := range definition.Requires {
					if ref.Kind != string(api.CephPool) {
						continue
					}
					for _, pool := range catalog.OfKind(api.CephPool) {
						if pool.Spec().Get("clusterRef").Text() == r.Cluster && nativePoolRef(catalog, pool.Name()) == ref.Object {
							definition.Requires[i].Object = pool.Name()
						}
					}
				}
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
			for _, ref := range []string{h.HostKeyRef, h.KnownHostsRef} {
				if ref != "" {
					plan.Secrets = append(plan.Secrets, ref)
				}
			}
		}
		for _, r := range requests {
			if r.Gateway != nil {
				for _, b := range r.Gateway.Bindings {
					plan.Secrets = append(plan.Secrets, b.SecretRef)
				}
				plan.Secrets = append(plan.Secrets, r.Gateway.authSecrets()...)
			}
			if r.Service != nil {
				for _, b := range r.Service.Bindings {
					plan.Secrets = append(plan.Secrets, b.SecretRef)
				}
			}
			if r.ObjectBucket != nil && r.ObjectBucket.CertificateRef != "" {
				plan.Secrets = append(plan.Secrets, r.ObjectBucket.CertificateRef)
			}
		}
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (c Capability) objectRequests(catalog api.Catalog, object api.Object, d derivation) ([]Request, error) {
	if c.kind == api.CephObjectGateway {
		return gatewayRequests(catalog, object, d)
	}
	request, err := c.request(catalog, object, d)
	if err != nil {
		return nil, err
	}
	requests := []Request{request}
	if c.kind == api.CephCluster {
		if settings, needed := settingsRequest(catalog, request, object); needed {
			requests = append(requests, settings)
		}
		units, err := serviceUnitRequests(request, object)
		if err != nil {
			return nil, err
		}
		if err := projectNVMeOFPools(catalog, object, units); err != nil {
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
	description := impacts[0]
	if request.Service != nil && verb == reconciliation.Apply && unitTypes[request.Service.Type].impacts != nil {
		impacts = append(impacts, unitTypes[request.Service.Type].impacts(object, *request.Service)...)
	}
	if request.Gateway != nil && verb == reconciliation.Apply {
		impacts = append(impacts, objectGatewayImpacts(*request.Gateway)...)
	}
	requires, dependencies, err := placement(object, request)
	if err != nil {
		return reconciliation.BlockDefinition{}, err
	}
	id := reconciliation.BlockIdentity("ceph-", request.Action+"-"+object.Name())
	if request.Service != nil {
		id = ServiceUnitBlockID(request.Cluster, *request.Service)
	}
	if c.kind == api.CephObjectGateway && request.Action == "pool" && request.Pool != nil {
		suffix, _ := zonePoolSuffix(request.Pool.Name)
		id = GatewayPoolBlockID(object.Name(), suffix)
	}
	return reconciliation.BlockDefinition{ID: id, Description: description, Stage: reconciliation.StageClusters, Requires: requires, Dependencies: dependencies, Exclusive: exclusiveFor(request.Action, request), Throttle: ThrottleKey(request.Cluster), Impacts: impacts, Consumes: consumes, Kind: string(c.kind), Object: object.Name(), Implementation: Implementation, ContentDigest: ContentDigest(), Request: canonical}, nil
}

var serializedFamilies = []string{"cluster", "consumer", "nvmeof", "replication"}

var serializedUnits = []string{"nvmeof", "rbd-mirror"}

var restartingUnits = []string{"monitoring"}

func OrchestratorKey(cluster string) string {
	return "ceph/" + cluster
}

func ManagerRestartKey(cluster string) string {
	return "ceph-mgr/" + cluster
}

func ObjectRootKey(cluster string) string {
	return "ceph-rgw/" + cluster
}

func ThrottleKey(cluster string) string {
	return "ceph/" + cluster
}

func exclusiveFor(family string, request Request) []string {
	serialized := []string{ManagerRestartKey(request.Cluster), OrchestratorKey(request.Cluster)}
	switch {
	case slices.Contains(serializedFamilies, family):
		return serialized
	case family == "service" && request.Service != nil && slices.Contains(serializedUnits, request.Service.Type):
		return serialized
	case family == "settings", family == "service" && request.Service != nil && slices.Contains(restartingUnits, request.Service.Type):
		return []string{ManagerRestartKey(request.Cluster)}
	case family == "gateway", family == "nfs-export" && request.NFSExport != nil && request.NFSExport.ObjectUser != nil:
		return []string{ObjectRootKey(request.Cluster)}
	}
	return nil
}

func placement(object api.Object, request Request) ([]reconciliation.ObjectRef, []string, error) {
	requires := []reconciliation.ObjectRef{}
	dependencies := []string{ClusterBlockID(request.Cluster), ImageBlockID(request.Cluster)}
	switch request.Action {
	case "cluster":
		dependencies = []string{ImageBlockID(request.Cluster)}
		requires = append(requires, hostRequirements(request.Controller, request.Hosts...)...)
		for _, host := range request.Hosts {
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
			unit, err := unitDependencies(request, object)
			if err != nil {
				return nil, nil, err
			}
			dependencies = unit
		}
	case "pool":
		if ref := object.Spec().Get("placementPolicyRef").Text(); ref != "" && object.Kind() == api.CephPool {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephPlacementPolicy), Object: ref})
		}
	case "client":
		requires = append(requires, clientRequires(*request.Client)...)
	case "rbd":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephPool), Object: request.RBD.Pool})
		if request.RBD.DataPool != "" {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephPool), Object: request.RBD.DataPool})
		}
	case "filesystem":
		for _, pool := range append([]string{request.Filesystem.MetadataPool}, request.Filesystem.DataPools...) {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephPool), Object: pool})
		}
	case "subvolume":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephFilesystem), Object: request.Subvolume.Filesystem})
	case "nvmeof":
		dependencies = append(dependencies, ServiceUnitBlockID(request.Cluster, ServiceUnit{Type: "nvmeof", ID: request.NVMeOF.Service}))
		for _, namespace := range request.NVMeOF.Namespaces {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephRBDImage), Object: namespace.Image.Name})
		}
	case "nfs-export":
		dependencies = append(dependencies, ServiceUnitBlockID(request.Cluster, ServiceUnit{Type: "nfs", ID: request.NFSExport.Service}))
		backing := reconciliation.ObjectRef{Kind: string(api.CephFilesystem), Object: request.NFSExport.Filesystem}
		if request.NFSExport.Subvolume != "" {
			backing = reconciliation.ObjectRef{Kind: string(api.CephSubvolume), Object: request.NFSExport.Subvolume}
		}
		if request.NFSExport.ObjectUser != nil {
			backing = reconciliation.ObjectRef{Kind: string(api.CephObjectUser), Object: request.NFSExport.ObjectUser.Name}
		}
		requires = append(requires, backing)
	case "object-user":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephObjectGateway), Object: request.ObjectUser.Gateway})
	case "object-bucket":
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephObjectUser), Object: request.ObjectBucket.Owner})
	case "gateway":
		for _, suffix := range derivedZonePools(object) {
			dependencies = append(dependencies, GatewayPoolBlockID(object.Name(), suffix))
		}
		for _, pool := range declaredPlacementPools(object) {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephPool), Object: pool})
		}
		if request.Gateway != nil && len(request.Gateway.Documents) > 1 && len(request.Auxiliary) != 0 {
			dependencies = append(dependencies, SettingsBlockID(request.Cluster))
		}
	}
	slices.Sort(dependencies)
	return requires, dependencies, nil
}

// Unsupported runs before operation registration and before secret custody.
func (c Capability) Unsupported(state *compilation.State) []lifecycle.Refusal {
	if state == nil {
		return nil
	}
	var out []lifecycle.Refusal
	for _, object := range state.Effective().OfKind(c.kind) {
		cluster := object
		if c.kind != api.CephCluster {
			cluster, _ = state.Effective().Find(api.CephCluster, object.Spec().Get("clusterRef").Text())
		}
		ceph := cluster.Spec()
		if hosts, stretch := len(ceph.Get("topology", "nodes").Items()), ceph.Has("topology", "stretch"); hosts > hostBound(stretch) {
			reason, remedy := clusterHostBudget(cluster.Name(), hosts, stretch)
			out = append(out, lifecycle.RefusalOf(object, reason, remedy))
		}
		if distribution := api.CephDistribution(ceph); distribution != "" && distribution != "ibm" {
			out = append(out, lifecycle.RefusalOf(object, distributionReason, distributionRemedy))
		}
		if !Supported(ceph.Get("release").Text(), ceph.Get("packageVersion").Text(), ceph.Get("cephadm", "ansible", "packageVersion").Text(),
			ceph.Get("image", "base").Text(), ceph.Get("image", "version").Text(), ceph.Get("cephadm", "clusterSSH", "transport").Text()) {
			out = append(out, lifecycle.RefusalOf(object, unsupportedReason, unsupportedRemedy))
		}
		if reason, remedy, gap := unselectedAuxiliary(cluster); gap {
			out = append(out, lifecycle.RefusalOf(object, reason, remedy))
		} else if reason, remedy, gap := unselectedSMBImages(state.Effective(), cluster); gap {
			out = append(out, lifecycle.RefusalOf(object, reason, remedy))
		} else if reason, remedy, gap := unneededAuxiliary(state.Effective(), cluster); gap {
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
	if r.NVMeOF != nil {
		return "NVMe-oF subsystem " + object
	}
	if r.NFSExport != nil {
		return "NFS export " + object
	}
	if r.ObjectUser != nil {
		return "object user " + object
	}
	if r.ObjectBucket != nil {
		return "object bucket " + object
	}
	if r.Pool != nil && r.Pool.Object == "" && r.Pool.Name != object {
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
	action := map[api.Kind]string{api.CephCluster: "cluster", api.CephPlacementPolicy: "policy", api.CephPool: "pool", api.CephClient: "client", api.CephRBDImage: "rbd", api.CephFilesystem: "filesystem", api.CephSubvolume: "subvolume", api.CephObjectGateway: "pool", api.CephNFSExport: "nfs-export", api.CephObjectUser: "object-user", api.CephObjectBucket: "object-bucket", api.CephNVMeoFSubsystem: "nvmeof"}[c.kind]
	object := r.Cluster
	if row := actions[r.Action]; row.object != nil && row.arm(r) {
		object = row.object(r)
	}
	if c.kind == api.CephObjectGateway {
		object = gatewayPoolObject(e.Block, r)
		if r.Action == "gateway" && r.Gateway != nil && e.Block.ID == GatewayBlockID(r.Gateway.Name) {
			action, object = r.Action, r.Gateway.Name
		}
	}
	if c.kind == api.CephCluster && (r.Action == "settings" || r.Action == "service") {
		action = r.Action
	}
	if action == "" || r.Action != action || !onlyOwnArm(r) || r.NFSExport != nil && r.NFSExport.SubvolumePath != "" || r.Client != nil && r.Client.SubvolumePath != "" || e.Block.Kind != string(c.kind) || e.Block.Object != object {
		return Request{}, cephRequestInvalid.Fail("Ceph request does not match its frozen resource identity", "")
	}
	if r.Client != nil {
		if caps, _ := scopeCaps(*r.Client); !maps.Equal(caps, r.Client.Caps) {
			return Request{}, cephOtherBuild.Fail("CephClient "+r.Client.Name+" froze capability grants this build does not derive", "destroy it with the build that applied it, then apply with this build")
		}
	}
	if r.Owner != "" || r.FSID != "" || e.ApplyOperation == "" {
		return Request{}, cephRecordsMissing.Fail("Ceph requires the immutable original apply identity", "")
	}
	if err := bindPhysicalHosts(r.Hosts, e, c.proofs); err != nil {
		return Request{}, err
	}
	r.Owner = e.ApplyOperation
	r.FSID = FSIDFor(e.ApplyOperation, r.Cluster)
	return r, nil
}

func onlyOwnArm(r Request) bool {
	for action, row := range actions {
		if row.arm != nil && row.arm(r) != (action == r.Action) {
			return false
		}
	}
	return true
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
	if r.Action == "cluster" {
		if err := proveDistinctHostDevices(e, r); err != nil {
			return Request{}, err
		}
	}
	if r.Auxiliary, err = ProvedAuxiliaryImages(proofs, r.Cluster, r.Auxiliary); err != nil {
		return Request{}, err
	}
	if r.NFSExport != nil && r.NFSExport.Subvolume != "" {
		path, filesystem, group, err := ProvedSubvolume(proofs, r.NFSExport.Subvolume)
		if err != nil {
			return Request{}, err
		}
		if filesystem != r.NFSExport.Filesystem || group != r.NFSExport.Group {
			return Request{}, cephProofMismatch.Fail("the creation proof of CephSubvolume "+r.NFSExport.Subvolume+" names another filesystem or group than CephNFSExport "+r.NFSExport.Name+" froze", "restore the original apply evidence")
		}
		r.NFSExport.SubvolumePath = path
	}
	if r.Client != nil && r.Client.Subvolume != "" {
		path, filesystem, group, err := ProvedSubvolume(proofs, r.Client.Subvolume)
		if err != nil {
			return Request{}, err
		}
		if filesystem != r.Client.Filesystem || group != r.Client.Group {
			return Request{}, cephProofMismatch.Fail("the creation proof of CephSubvolume "+r.Client.Subvolume+" names another filesystem or group than CephClient "+r.Client.Name+" froze", "restore the original apply evidence")
		}
		client := *r.Client
		client.SubvolumePath = path
		client.Caps = maps.Clone(client.Caps)
		client.Caps["mds"] += " path=" + path
		r.Client = &client
	}
	return r, nil
}

func clientRequires(c Client) []reconciliation.ObjectRef {
	switch {
	case c.Subvolume != "":
		return []reconciliation.ObjectRef{{Kind: string(api.CephSubvolume), Object: c.Subvolume}}
	case c.Filesystem != "":
		return []reconciliation.ObjectRef{{Kind: string(api.CephFilesystem), Object: c.Filesystem}}
	}
	requires := []reconciliation.ObjectRef{{Kind: string(api.CephPool), Object: c.Pool}}
	for _, pool := range c.DataPools {
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.CephPool), Object: pool})
	}
	return requires
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
	if operation == "destroy" && physicalHosts(r.Hosts) {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, physicalRemovalRefusal()
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
	effect, err := evaluateExecutionEvidence(result.Evidence, r, e, operation == "destroy")
	if err != nil || effect != reconciliation.EffectCompleted {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, cephUnproved.Fail("Ceph did not prove the requested postcondition", "")
	}
	outcome, usable := result.Outcome, result.Outcome.Settled()
	if !usable {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, cephResultInvalid.Fail("Ceph returned an unknown outcome", "")
	}
	if err := validateProduced(result.Produced, r, operation == "apply"); err != nil {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, err
	}
	if err := identifyProduced(result.Produced, r, result.Evidence); err != nil {
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
	var result lifecycle.RunResult
	if shared := c.sharedHosts(ctx, e, r, removal); shared != nil {
		result, err = c.invoke(ctx, e, "observe", r, shared, nil)
	} else {
		result, err = c.runFor(ctx, e, "observe", r, removal)
	}
	if err != nil {
		return unknown, err
	}
	effect, err := evaluateExecutionEvidence(result.Evidence, r, e, removal)
	if err != nil {
		lifecycle.ClearProduced(result.Produced)
		return unknown, err
	}
	if effect != reconciliation.EffectCompleted || removal {
		lifecycle.ClearProduced(result.Produced)
		result.Produced = nil
	} else if err := validateObserved(result.Produced, r, lifecycle.Recorded(e)); err != nil {
		lifecycle.ClearProduced(result.Produced)
		return unknown, err
	} else if err := identifyProduced(result.Produced, r, result.Evidence); err != nil {
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
		if r, err := DecodeRequest(probe.Block.Request); err == nil && len(installedMachines(r)) != 0 {
			if physicalHosts(r.Hosts) {
				if err := authorizePhysicalRetirement(probe.Execution(), r); err != nil {
					return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: "physical retirement requires every host's original erase policy and a terminal verified erase receipt"}, nil
				}
				return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: pendingPhysicalReason}, nil
			}
			return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: retiredReason}, nil
		}
	}
	return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
}
