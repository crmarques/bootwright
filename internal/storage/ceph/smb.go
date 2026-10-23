package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	SMBImplementation  = "storage-ceph-smb-v1"
	SMBVersion         = "ceph-smb-v1"
	SMBEvidenceVersion = "ceph-smb-evidence-v1"
	SMBManagerModule   = "smb"
)

func SMBContentDigest() string {
	d := sha256.Sum256([]byte(SMBImplementation + "\x00" + SMBVersion + "\x00" + SMBEvidenceVersion))
	return hex.EncodeToString(d[:])
}

func SMBBlockID(object string) string { return "ceph-smb-" + object }

type SMBBinding struct {
	Path      string `json:"path"`
	SecretRef string `json:"secretRef"`
}

type SMBImage struct {
	Image string `json:"image"`
	Name  string `json:"name"`
}

type SMBService struct {
	Clustered bool     `json:"clustered"`
	Hosts     []string `json:"hosts"`
	Name      string   `json:"name"`
}

type SMBRequest struct {
	Bindings     []SMBBinding    `json:"bindings"`
	Cluster      string          `json:"cluster"`
	ClusterIDs   []string        `json:"clusterIDs,omitempty"`
	Controller   string          `json:"controller"`
	ID           string          `json:"id"`
	Images       []SMBImage      `json:"images,omitempty"`
	Machines     []string        `json:"machines"`
	Native       json.RawMessage `json:"native"`
	Object       string          `json:"object"`
	ResourceType string          `json:"resourceType"`
	Scope        json.RawMessage `json:"scope,omitempty"`
	Service      *SMBService     `json:"service,omitempty"`
	Version      string          `json:"version"`
}

var smbIdentityLeaves = map[string]string{api.SMBCluster: "cluster_id", api.SMBShare: "share_id", api.SMBJoinAuth: "auth_id", api.SMBUsersGroups: "users_groups_id"}

var smbSubjects = map[string]string{api.SMBCluster: "SMB cluster", api.SMBShare: "SMB share", api.SMBJoinAuth: "SMB join auth", api.SMBUsersGroups: "SMB users and groups"}

var smbUserPassword = regexp.MustCompile(`\A/values/users/(?:0|[1-9][0-9]{0,3})/password\z`)

func (r SMBRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "Ceph SMB") }

func DecodeSMBRequest(data []byte) (SMBRequest, error) {
	r, err := reconciliation.ThawVersion[SMBRequest](data, "Ceph SMB", SMBVersion)
	if err != nil {
		return SMBRequest{}, err
	}
	if !r.consistent() {
		return SMBRequest{}, failure("lifecycle.state", "the frozen Ceph SMB request is inconsistent", "")
	}
	return r, nil
}

func strictlySorted(values []string) bool {
	for i, value := range values {
		if value == "" || i > 0 && values[i-1] >= value {
			return false
		}
	}
	return len(values) != 0
}

func (r SMBRequest) nativeLeaf(name string) string {
	members, err := reconciliation.Thaw[map[string]json.RawMessage](r.Native, "Ceph SMB native")
	if err != nil {
		return ""
	}
	value, err := reconciliation.Thaw[string](members[name], "Ceph SMB native member")
	if err != nil {
		return ""
	}
	return value
}

func (r SMBRequest) consistent() bool {
	leaf, known := smbIdentityLeaves[r.ResourceType]
	if !known || r.Cluster == "" || r.Controller == "" || r.Object == "" || r.ID == "" || len(r.Scope) != 0 || !reconciliation.ValidSegment(SMBBlockID(r.Object)) ||
		!strictlySorted(r.Machines) || r.Bindings == nil || canonicaljson.ProveObject(r.Native) != nil || r.nativeLeaf("resource_type") != r.ResourceType || r.nativeLeaf(leaf) != r.ID {
		return false
	}
	switch r.ResourceType {
	case api.SMBCluster, api.SMBShare:
		return len(r.Bindings) == 0 && r.serviceConsistent() && r.imagesConsistent()
	case api.SMBJoinAuth:
		return r.Service == nil && len(r.ClusterIDs) == 0 && len(r.Images) == 0 && len(r.Bindings) == 1 && r.Bindings[0].Path == "/auth/password" && r.Bindings[0].SecretRef != ""
	}
	return r.Service == nil && len(r.ClusterIDs) == 0 && len(r.Images) == 0 && r.usersBindings()
}

func (r SMBRequest) serviceConsistent() bool {
	if r.Service == nil || !strictlySorted(r.ClusterIDs) || !strictlySorted(r.Service.Hosts) {
		return false
	}
	id, named := strings.CutPrefix(r.Service.Name, "smb.")
	own := r.ID
	if r.ResourceType == api.SMBShare {
		own = r.nativeLeaf("cluster_id")
	}
	return named && slices.Contains(r.ClusterIDs, id) && id == own
}

func (r SMBRequest) imagesConsistent() bool {
	if len(r.Images) == 0 {
		return true
	}
	return len(r.Images) == 2 && r.Images[0].Name == "samba" && r.Images[1].Name == "samba_metrics" && r.Images[0].Image != "" && r.Images[1].Image != ""
}

func (r SMBRequest) usersBindings() bool {
	for i, b := range r.Bindings {
		if b.SecretRef == "" || !smbUserPassword.MatchString(b.Path) || i > 0 && r.Bindings[i-1].Path >= b.Path {
			return false
		}
	}
	return len(r.Bindings) != 0
}

func smbSubject(r SMBRequest) string {
	return smbSubjects[r.ResourceType] + " " + r.ID + " of " + r.Object
}

func smbRemoval(verb reconciliation.Verb, r SMBRequest) lifecycle.Removal {
	if verb == reconciliation.Destroy {
		return lifecycle.Removal{Description: "remove Ceph " + smbSubject(r), Impacts: []string{"remove Ceph " + smbSubject(r)}, Consumes: []string{reconciliation.AuthorizationDataLoss}}
	}
	return lifecycle.Removal{Description: "realize Ceph " + smbSubject(r), Impacts: []string{"realize Ceph " + smbSubject(r)}, Consumes: []string{}}
}

func smbValue(v api.Value) (any, bool) {
	switch v.Type() {
	case api.Mapping:
		out := map[string]any{}
		for _, field := range v.Fields() {
			value, ok := smbValue(field.Value)
			if !ok {
				return nil, false
			}
			out[field.Name] = value
		}
		return out, true
	case api.Sequence:
		out := []any{}
		for _, item := range v.Items() {
			value, ok := smbValue(item)
			if !ok {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	case api.String:
		return v.Text(), true
	case api.Integer:
		return v.Int64()
	case api.Boolean:
		return v.Bool(), true
	}
	return nil, false
}

func smbNative(c api.Catalog, cluster, object api.Object) (json.RawMessage, error) {
	native := object.Spec().Get("native")
	value, ok := smbValue(native)
	document, mapped := value.(map[string]any)
	if !ok || !mapped {
		return nil, failure("lifecycle.state", "StorageSMBResource "+object.Name()+" holds a native value Bootwright cannot freeze", "")
	}
	switch native.Get("resource_type").Text() {
	case api.SMBCluster:
		hosts, err := serviceHosts(cluster, native)
		if err != nil {
			return nil, err
		}
		slices.Sort(hosts)
		placement := map[string]any{"hosts": hosts}
		if n, ok := native.Get("placement", "count").Int64(); ok {
			placement["count"] = n
		}
		if n, ok := native.Get("placement", "count_per_host").Int64(); ok {
			placement["count_per_host"] = n
		}
		document["placement"] = placement
	case api.SMBShare:
		cephfs, _ := document["cephfs"].(map[string]any)
		if cephfs == nil {
			cephfs = map[string]any{}
		}
		cephfs["volume"] = object.Spec().Get("filesystemRef").Text()
		if ref := object.Spec().Get("subvolumeRef").Text(); ref != "" {
			subvolume, _ := c.Find(api.StorageSubvolume, ref)
			cephfs["volume"], cephfs["subvolume"] = subvolume.Spec().Get("filesystemRef").Text(), subvolume.Name()
			if group := subvolume.Spec().Get("group").Text(); group != "" {
				cephfs["subvolumegroup"] = group
			}
		}
		document["cephfs"] = cephfs
	}
	return json.Marshal(document)
}

func smbClustered(native api.Value) bool {
	switch native.Get("clustering").Text() {
	case "always":
		return true
	case "never":
		return false
	}
	count, ok := native.Get("placement", "count").Int64()
	return !ok || count != 1
}

func smbServiceOf(cluster, smbCluster api.Object) (*SMBService, error) {
	native := smbCluster.Spec().Get("native")
	hosts, err := serviceHosts(cluster, native)
	if err != nil {
		return nil, err
	}
	slices.Sort(hosts)
	return &SMBService{Clustered: smbClustered(native), Hosts: hosts, Name: "smb." + native.Get("cluster_id").Text()}, nil
}

func smbClusterIDs(c api.Catalog, cluster string) []string {
	ids := []string{}
	for _, object := range clusterSMBResources(c, cluster) {
		if object.Spec().Get("native", "resource_type").Text() == api.SMBCluster {
			ids = append(ids, object.Spec().Get("native", "cluster_id").Text())
		}
	}
	slices.Sort(ids)
	return ids
}

func smbDelegated(c api.Catalog, cluster api.Object) ([]DelegatedService, error) {
	var out []DelegatedService
	for _, object := range clusterSMBResources(c, cluster.Name()) {
		if object.Spec().Get("native", "resource_type").Text() != api.SMBCluster {
			continue
		}
		service, err := smbServiceOf(cluster, object)
		if err != nil {
			return nil, err
		}
		out = append(out, DelegatedService{Name: service.Name, Hosts: service.Hosts})
	}
	return out, nil
}

func smbImages(auxiliary []AuxiliaryReference) []SMBImage {
	var images []SMBImage
	for _, a := range auxiliary {
		if a.Name == "samba" || a.Name == "samba_metrics" {
			images = append(images, SMBImage{Image: a.Image, Name: a.Name})
		}
	}
	return images
}

func smbRequest(c api.Catalog, object api.Object, base Request, controller string) (SMBRequest, error) {
	spec := object.Spec()
	rt := spec.Get("native", "resource_type").Text()
	cluster, _ := c.Find(api.StorageCluster, base.Cluster)
	r := SMBRequest{Version: SMBVersion, Object: object.Name(), Cluster: base.Cluster, Controller: controller, ResourceType: rt, ID: spec.Get("native", smbIdentityLeaves[rt]).Text(),
		Machines: requestMachines(base), Bindings: []SMBBinding{}}
	native, err := smbNative(c, cluster, object)
	if err != nil {
		return SMBRequest{}, err
	}
	r.Native = native
	switch rt {
	case api.SMBJoinAuth, api.SMBUsersGroups:
		for _, binding := range spec.Get("secrets").Items() {
			r.Bindings = append(r.Bindings, SMBBinding{Path: binding.Get("path").Text(), SecretRef: binding.Get("secretRef").Text()})
		}
		slices.SortFunc(r.Bindings, func(a, b SMBBinding) int { return strings.Compare(a.Path, b.Path) })
	case api.SMBCluster, api.SMBShare:
		smbCluster := object
		if rt == api.SMBShare {
			smbCluster, _ = c.Find(api.StorageSMBResource, spec.Get("smbClusterRef").Text())
		}
		if r.Service, err = smbServiceOf(cluster, smbCluster); err != nil {
			return SMBRequest{}, err
		}
		r.ClusterIDs, r.Images = smbClusterIDs(c, base.Cluster), smbImages(base.Auxiliary)
	}
	return r, nil
}

func smbRequires(spec api.Value, rt string) []reconciliation.ObjectRef {
	requires := []reconciliation.ObjectRef{}
	switch rt {
	case api.SMBShare:
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageSMBResource), Object: spec.Get("smbClusterRef").Text()})
		if ref := spec.Get("subvolumeRef").Text(); ref != "" {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageSubvolume), Object: ref})
		} else {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageFilesystem), Object: spec.Get("filesystemRef").Text()})
		}
	case api.SMBCluster:
		for _, member := range spec.Get("resourceRefs").Strings() {
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageSMBResource), Object: member})
		}
	}
	slices.SortFunc(requires, func(a, b reconciliation.ObjectRef) int {
		return strings.Compare(a.Kind+"/"+a.Object, b.Kind+"/"+b.Object)
	})
	return slices.Compact(requires)
}

type SMBCapability struct {
	runner Runner
	proofs DependencyProofs
}

func NewSMB(runner Runner) SMBCapability { return SMBCapability{runner: runner} }

func (c SMBCapability) WithProofs(proofs DependencyProofs) SMBCapability {
	c.proofs = proofs
	return c
}

func (c SMBCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, failure("lifecycle.state", "Ceph planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.StorageSMBResource) {
		definition, secrets, err := smbDefinition(catalog, object, input)
		if err != nil {
			return plan, err
		}
		plan.Definitions = append(plan.Definitions, definition)
		plan.Secrets = append(plan.Secrets, secrets...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func smbDefinition(c api.Catalog, object api.Object, input lifecycle.PlanInput) (reconciliation.BlockDefinition, []string, error) {
	if !reconciliation.ValidSegment(SMBBlockID(object.Name())) {
		return reconciliation.BlockDefinition{}, nil, failure("api.invariant", "StorageSMBResource "+object.Name()+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageSMBResource to at most 54 characters")
	}
	cluster, _ := c.Find(api.StorageCluster, object.Spec().Get("clusterRef").Text())
	base, err := baseRequest(c, cluster, input.Context.Name)
	if err != nil {
		return reconciliation.BlockDefinition{}, nil, err
	}
	r, err := smbRequest(c, object, base, input.Controller)
	if err != nil {
		return reconciliation.BlockDefinition{}, nil, err
	}
	canonical, err := r.Canonical()
	if err != nil {
		return reconciliation.BlockDefinition{}, nil, err
	}
	if _, err := DecodeSMBRequest(canonical); err != nil {
		return reconciliation.BlockDefinition{}, nil, err
	}
	removal := smbRemoval(input.Verb, r)
	dependencies := []string{ClusterBlockID(r.Cluster), ImageBlockID(r.Cluster), SettingsBlockID(r.Cluster)}
	slices.Sort(dependencies)
	secrets := []string{}
	for _, b := range r.Bindings {
		secrets = append(secrets, b.SecretRef)
	}
	for _, h := range base.Hosts {
		secrets = append(secrets, h.SSHKeyRef)
		if h.HostKeyRef != "" {
			secrets = append(secrets, h.HostKeyRef)
		}
	}
	return reconciliation.BlockDefinition{ID: SMBBlockID(object.Name()), Description: removal.Description, Stage: reconciliation.StageClusters, Requires: smbRequires(object.Spec(), r.ResourceType),
		Dependencies: dependencies, Exclusive: []string{"ceph/" + r.Cluster}, Impacts: removal.Impacts, Consumes: removal.Consumes, Kind: string(api.StorageSMBResource), Object: object.Name(),
		Implementation: SMBImplementation, ContentDigest: SMBContentDigest(), Request: canonical}, secrets, nil
}

func (SMBCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeSMBRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	return smbRemoval(reconciliation.Destroy, r), nil
}

func (SMBCapability) RequiresOnlineRemoval(reconciliation.Block) bool { return true }

func (SMBCapability) identity(ctx context.Context, e lifecycle.Execution) (SMBRequest, error) {
	if err := ctx.Err(); err != nil {
		return SMBRequest{}, err
	}
	r, err := DecodeSMBRequest(e.Block.Request)
	if err != nil {
		return SMBRequest{}, err
	}
	if e.Block.Kind != string(api.StorageSMBResource) || e.Block.Object != r.Object || e.Block.ID != SMBBlockID(r.Object) {
		return SMBRequest{}, failure("lifecycle.state", "Ceph SMB request does not match its frozen resource identity", "")
	}
	if e.ApplyOperation == "" {
		return SMBRequest{}, failure("lifecycle.state", "Ceph SMB requires the immutable original apply identity", "")
	}
	return r, nil
}

func (c SMBCapability) prepare(ctx context.Context, e lifecycle.Execution) (SMBRequest, ProvedClusterScope, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return SMBRequest{}, ProvedClusterScope{}, err
	}
	scope, err := ProvedCluster(provedFor(e), r.Cluster, e.ApplyOperation)
	if err != nil {
		return SMBRequest{}, ProvedClusterScope{}, err
	}
	if !slices.Equal(r.Machines, scope.Machines) {
		return SMBRequest{}, ProvedClusterScope{}, failure("lifecycle.state", "StorageSMBResource "+r.Object+" froze another Machine set than the creation proof of StorageCluster "+r.Cluster, "restore the original apply evidence")
	}
	if r.Service != nil && len(r.Images) == 0 {
		return SMBRequest{}, ProvedClusterScope{}, failure("lifecycle.state", "StorageSMBResource "+r.Object+" froze no samba image", "")
	}
	for i, image := range r.Images {
		authored := slices.IndexFunc(scope.Authored, func(a AuxiliaryReference) bool { return a.Name == image.Name })
		completed := slices.IndexFunc(scope.Scope.Auxiliary, func(a AuxiliaryReference) bool { return a.Name == image.Name })
		if authored < 0 || completed < 0 || scope.Authored[authored].Image != image.Image {
			return SMBRequest{}, ProvedClusterScope{}, failure("lifecycle.state", "StorageSMBResource "+r.Object+" froze a samba image its StorageCluster's settings did not freeze", "restore the original apply evidence")
		}
		r.Images[i].Image = scope.Scope.Auxiliary[completed].Image
	}
	if err := bindPhysicalHosts(scope.Scope.Hosts, e, c.proofs); err != nil {
		return SMBRequest{}, ProvedClusterScope{}, err
	}
	if r.Scope, err = reconciliation.Freeze(scope.Scope, "Ceph SMB cluster scope"); err != nil {
		return SMBRequest{}, ProvedClusterScope{}, err
	}
	return r, scope, nil
}

func smbDeadline(r SMBRequest, operation string) (time.Duration, error) {
	calls, waits := 0, 0
	switch operation {
	case "apply":
		calls = 3
		if r.ResourceType == api.SMBShare {
			calls, waits = 4, 1
		}
	case "observe":
		calls = 1
		if r.ResourceType == api.SMBShare {
			calls = 2
		}
	default:
		return 0, failure("lifecycle.state", "the Ceph SMB adapter runs no "+operation+" operation", "")
	}
	return time.Duration(calls*nativeCall+waits*readinessWait+60) * time.Second, nil
}

func (c SMBCapability) run(ctx context.Context, e lifecycle.Execution, operation string, r SMBRequest, scope ProvedClusterScope) (lifecycle.RunResult, error) {
	if c.runner == nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the Ceph SMB adapter is not configured", "")
	}
	canonical, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	deadline, err := smbDeadline(r, operation)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	execution, files, cleanup, err := smbMaterial(e, r, scope.Scope, operation, c.proofs)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	defer cleanup()
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: SMBImplementation, Operation: operation, Variable: "bootwright_ceph_smb", Canonical: canonical,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Controller}, Materials: files, Refusals: smbRefusals(), NativeErrors: true, Deadline: deadline}))
}

func (c SMBCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	r, scope, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, e, "apply", r, scope)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	effect, err := EvaluateSMBEvidence(result.Evidence, r, e.ApplyOperation, FSIDFor(e.ApplyOperation, r.Cluster), e.Block.RequestDigest, false)
	if err != nil || effect != reconciliation.EffectCompleted || len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "Ceph SMB did not prove the requested postcondition", "")
	}
	outcome, known := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	if !known {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "Ceph SMB returned an unknown outcome", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c SMBCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	r, scope, err := c.prepare(ctx, e)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, e, "observe", r, scope)
	if err != nil {
		return unknown, err
	}
	if len(result.Produced) != 0 {
		lifecycle.ClearProduced(result.Produced)
		return unknown, failure("lifecycle.unknown", "Ceph SMB returned a value it never produces", "")
	}
	effect, err := EvaluateSMBEvidence(result.Evidence, r, e.ApplyOperation, FSIDFor(e.ApplyOperation, r.Cluster), e.Block.RequestDigest, false)
	if err != nil {
		return unknown, err
	}
	return lifecycle.Observation{Effect: effect, Evidence: result.Evidence}, nil
}

func (c SMBCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	if e.RetiredWithHosts {
		return c.retire(ctx, e)
	}
	if _, err := c.identity(ctx, e); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, failure("lifecycle.unsupported", survivingReason, survivingRemedy)
}

func (c SMBCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	if e.RetiredWithHosts {
		return c.observeRetirement(ctx, e)
	}
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	if _, err := c.identity(ctx, e); err != nil {
		return unknown, err
	}
	return unknown, failure("lifecycle.unsupported", survivingReason, survivingRemedy)
}

func (SMBCapability) Quiescent(ctx context.Context, probe lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	if probe.RetiredWithHosts {
		if r, err := DecodeSMBRequest(probe.Block.Request); err == nil && len(r.Machines) != 0 {
			return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: retiredReason}, nil
		}
	}
	return lifecycle.Quiescence{State: lifecycle.Unproved, Reason: survivingReason, Remedy: survivingRemedy}, nil
}
