package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const HostImplementation = "storage-ceph-host-v1"
const HostVersion = "ceph-host-v6"
const HostEvidenceVersion = "ceph-host-evidence-v2"
const SharedLabel = "system_u:object_r:container_file_t:s0"
const maxHostEvidence = 16384

type ClusterScope struct {
	AnsiblePackageVersion    string                       `json:"ansiblePackageVersion"`
	Auxiliary                []AuxiliaryReference         `json:"auxiliary,omitempty"`
	Bootstrap                string                       `json:"bootstrap"`
	CallHome                 string                       `json:"callHome"`
	Cephadm                  Tool                         `json:"cephadm"`
	Cluster                  string                       `json:"cluster"`
	ClusterKeyRef            string                       `json:"clusterKeyRef"`
	ClusterNetworks          []string                     `json:"clusterNetworks"`
	ClusterSSHTransport      string                       `json:"clusterSSHTransport"`
	ClusterSSHUser           string                       `json:"clusterSSHUser"`
	Config                   map[string]map[string]string `json:"config,omitempty"`
	Controller               string                       `json:"controller"`
	FSID                     string                       `json:"fsid,omitempty"`
	Hosts                    []Host                       `json:"hosts"`
	Image                    string                       `json:"image"`
	MonIP                    string                       `json:"monIP"`
	NFSGaneshaPackageVersion string                       `json:"nfsGaneshaPackageVersion,omitempty"`
	Owner                    string                       `json:"owner,omitempty"`
	PackageRepository        string                       `json:"packageRepository"`
	Packages                 *PackageRequest              `json:"packages,omitempty"`
	PublicNetworks           []string                     `json:"publicNetworks"`
	Registry                 string                       `json:"registry"`
	RegistryRef              string                       `json:"registryRef"`
	RegistryTrustBundleRef   string                       `json:"registryTrustBundleRef,omitempty"`
	Release                  string                       `json:"release"`
	Security                 *Security                    `json:"security,omitempty"`
	Services                 []json.RawMessage            `json:"services"`
	Version                  string                       `json:"version"`
}

type HostRequest struct {
	Cluster ClusterScope `json:"cluster"`
	Host    string       `json:"host"`
	Version string       `json:"version"`
}

func (r HostRequest) canonical() ([]byte, error) { return reconciliation.Freeze(r, "Ceph host") }

func DecodeHostRequest(data []byte) (HostRequest, error) {
	r, err := reconciliation.ThawVersion[HostRequest](data, "Ceph host", HostVersion)
	if err != nil {
		return HostRequest{}, err
	}
	if r.Cluster.Version != Version || r.Cluster.Packages == nil {
		return HostRequest{}, failure("lifecycle.state", "the frozen Ceph host request does not carry its cluster's package-bearing scope", "")
	}
	if _, ok := r.target(); !ok {
		return HostRequest{}, failure("lifecycle.state", "the frozen Ceph host request names a host outside its cluster", "")
	}
	if _, ok := r.source(); !ok {
		return HostRequest{}, failure("lifecycle.state", "the frozen Ceph host request has no package source for its host", "")
	}
	return r, nil
}

func (r HostRequest) target() (Host, bool) {
	for _, h := range r.Cluster.Hosts {
		if h.Name == r.Host {
			return h, true
		}
	}
	return Host{}, false
}

func (r HostRequest) source() (int, bool) {
	if r.Cluster.Packages == nil {
		return 0, false
	}
	for i, source := range r.Cluster.Packages.Sources {
		if source.Host == r.Host {
			return i, true
		}
	}
	return 0, false
}

func HostContentDigest() string {
	d := sha256.Sum256([]byte(HostImplementation + "\x00" + HostVersion + "\x00" + HostEvidenceVersion + "\x00" + "selected-packages-pull-profile-route-devices-v2"))
	return hex.EncodeToString(d[:])
}

func HostBlockID(cluster, machineName string) string {
	return "ceph-host-" + cluster + "-" + machineName
}

type HostDevice struct {
	DeviceID string `json:"deviceID"`
	Path     string `json:"path"`
	Serial   string `json:"serial"`
}

type HostEvidence struct {
	CephadmSHA256   string       `json:"cephadmSHA256"`
	Devices         []HostDevice `json:"devices"`
	FSID            string       `json:"fsid"`
	GID             int          `json:"gid"`
	Host            string       `json:"host"`
	Image           string       `json:"image"`
	ImageID         string       `json:"imageID"`
	Label           string       `json:"label"`
	Owner           string       `json:"owner"`
	PlanDigest      string       `json:"planDigest"`
	PolicySHA256    string       `json:"policySHA256"`
	Ready           bool         `json:"ready"`
	Removed         bool         `json:"removed"`
	Request         string       `json:"request"`
	Route           bool         `json:"route"`
	Specializations []string     `json:"specializations,omitempty"`
	UID             int          `json:"uid"`
	Version         string       `json:"version"`
}

func hex64(value string) bool {
	return len(value) == 64 && strings.Trim(value, "0123456789abcdef") == ""
}

func validDeviceID(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("_.:+/-", c)) {
			return false
		}
	}
	return true
}

func decodeHostEvidence(data []byte, digest string) (HostEvidence, error) {
	evidence, err := reconciliation.DecodeEvidence[HostEvidence](data, maxHostEvidence, "Ceph host")
	if err != nil {
		return HostEvidence{}, err
	}
	if evidence.Version != HostEvidenceVersion || digest == "" || evidence.Request != digest {
		return HostEvidence{}, failure("lifecycle.state", "Ceph host evidence names another request", "")
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, maxHostEvidence, "Ceph host members")
	if raw, present := members["specializations"]; err != nil || present && (string(raw) == "null" || !validSpecializations(evidence.Specializations)) {
		return HostEvidence{}, failure("lifecycle.state", "Ceph host evidence names malformed specialization ids", "")
	}
	return evidence, nil
}

func (e HostEvidence) identifies(r HostRequest) bool {
	return e.Host == r.Host && e.Owner == r.Cluster.Owner && e.FSID == r.Cluster.FSID && e.Image == r.Cluster.Image
}

func (e HostEvidence) empty() bool {
	return !e.Ready && !e.Removed && !e.Route && e.ImageID == "" && e.UID == 0 && e.GID == 0 && e.Label == "" && e.PolicySHA256 == "" && e.CephadmSHA256 == "" && e.PlanDigest == "" && len(e.Devices) == 0
}

func (e HostEvidence) prepared(r HostRequest) bool {
	host, ok := r.target()
	if !ok || !e.Ready || e.Removed || !e.Route || !hex64(e.ImageID) || e.UID <= 0 || e.UID != e.GID || e.UID >= 65536 || e.Label != SharedLabel ||
		!hex64(e.PolicySHA256) || !hex64(e.CephadmSHA256) || !hex64(e.PlanDigest) || len(e.Devices) != len(host.Devices) {
		return false
	}
	for i, device := range host.Devices {
		if e.Devices[i].Path != device.Path || e.Devices[i].Serial != device.Serial || !validDeviceID(e.Devices[i].DeviceID) {
			return false
		}
	}
	return true
}

func hostEffect(e HostEvidence, r HostRequest) reconciliation.EffectState {
	switch {
	case !e.identifies(r) || e.Removed:
		return reconciliation.EffectUnknown
	case e.empty():
		return reconciliation.EffectNoEffect
	case e.prepared(r):
		return reconciliation.EffectCompleted
	case e.Ready:
		return reconciliation.EffectUnknown
	}
	return reconciliation.EffectPartial
}

type HostCapability struct {
	runner Runner
	proofs DependencyProofs
}

func NewHost(runner Runner) HostCapability { return HostCapability{runner: runner} }

func (c HostCapability) WithProofs(proofs DependencyProofs) HostCapability {
	c.proofs = proofs
	return c
}

func hostDescription(verb reconciliation.Verb, cluster, host string) (string, []string) {
	if verb == reconciliation.Destroy {
		return "release Ceph host preparation " + host + " of " + cluster, []string{"release-host " + host + " (packages, image and the Bootwright host runtime and package records retained)"}
	}
	return "prepare Ceph host " + host + " of " + cluster, []string{"prepare-host " + host + " packages, image, runtime, route and devices"}
}

func (c HostCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, failure("lifecycle.state", "Ceph planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.StorageCluster) {
		cluster, err := baseRequest(catalog, object, input.Context.Name)
		if err != nil {
			return plan, err
		}
		cluster.Controller = input.Controller
		cluster.Packages, err = packageSources(catalog, cluster, input.Context.Name)
		if err != nil {
			return plan, err
		}
		for _, host := range cluster.Hosts {
			request := HostRequest{Version: HostVersion, Cluster: scopeOf(cluster), Host: host.Name}
			index, _ := request.source()
			source := cluster.Packages.Sources[index]
			canonical, err := request.canonical()
			if err != nil {
				return plan, err
			}
			description, impacts := hostDescription(input.Verb, cluster.Cluster, host.Name)
			requires := []reconciliation.ObjectRef{hostRequirement(host)}
			for _, profile := range cluster.Packages.Sources {
				requires = append(requires, reconciliation.ObjectRef{Kind: string(api.MachineInstallProfile), Object: profile.Profile})
			}
			slices.SortFunc(requires, func(a, b reconciliation.ObjectRef) int {
				return strings.Compare(a.Kind+"/"+a.Object, b.Kind+"/"+b.Object)
			})
			requires = slices.Compact(requires)
			plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: HostBlockID(cluster.Cluster, host.Machine), Description: description, Stage: reconciliation.StageClusters,
				Requires: requires, Dependencies: []string{ImageBlockID(cluster.Cluster)}, Exclusive: []string{"ceph-host/" + host.Machine}, Impacts: impacts, Consumes: []string{},
				Kind: string(api.StorageCluster), Object: cluster.Cluster, Implementation: HostImplementation, ContentDigest: HostContentDigest(), Request: canonical})
			if cluster.Security != nil && cluster.Security.FIPS {
				plan.Secrets = append(plan.Secrets, cluster.ClusterKeyRef)
			}
			plan.Secrets = append(plan.Secrets, cluster.RegistryRef, host.SSHKeyRef, source.CertificateRef)
			if host.HostKeyRef != "" {
				plan.Secrets = append(plan.Secrets, host.HostKeyRef)
			}
			if cluster.RegistryTrustBundleRef != "" {
				plan.Secrets = append(plan.Secrets, cluster.RegistryTrustBundleRef)
			}
			if ref := cluster.Packages.PackageRepositoryTrustBundleRef; ref != "" {
				plan.Secrets = append(plan.Secrets, ref)
			}
		}
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}

func (HostCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Removal{}, err
	}
	r, err := DecodeHostRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	description, impacts := hostDescription(reconciliation.Destroy, r.Cluster.Cluster, r.Host)
	return lifecycle.Removal{Description: description, Impacts: impacts, Consumes: []string{}}, nil
}

func (c HostCapability) prepare(ctx context.Context, e lifecycle.Execution) (HostRequest, error) {
	if err := ctx.Err(); err != nil {
		return HostRequest{}, err
	}
	r, err := DecodeHostRequest(e.Block.Request)
	if err != nil {
		return HostRequest{}, err
	}
	if e.Block.Kind != string(api.StorageCluster) || e.Block.Object != r.Cluster.Cluster {
		return HostRequest{}, failure("lifecycle.state", "Ceph host request does not match its frozen resource identity", "")
	}
	if r.Cluster.Owner != "" || r.Cluster.FSID != "" || e.ApplyOperation == "" {
		return HostRequest{}, failure("lifecycle.state", "Ceph host preparation requires the immutable original apply identity", "")
	}
	r.Cluster.Owner = e.ApplyOperation
	r.Cluster.FSID = FSIDFor(e.ApplyOperation, r.Cluster.Cluster)
	image, err := ProvedImage(provedFor(e), r.Cluster.Cluster, r.Cluster.Image)
	if err != nil {
		return HostRequest{}, err
	}
	r.Cluster.Image = image
	if r.Cluster.Auxiliary, err = ProvedAuxiliaryImages(provedFor(e), r.Cluster.Cluster, r.Cluster.Auxiliary); err != nil {
		return HostRequest{}, err
	}
	return r, nil
}

func provedFor(e lifecycle.Execution) []lifecycle.BlockEvidence {
	if len(e.ApplyProved) == 0 {
		return e.Proved
	}
	return e.ApplyProved
}

func hostRefusals() map[string]error {
	out := map[string]error{"unsupported-release": unsupportedFailure(), "fips-policy": adapterRefusals()["fips-policy"]}
	for reason, message := range map[string]string{
		"image-pull":      "the frozen Ceph image or an auxiliary image digest could not be pulled within its three bounded attempts",
		"image-platform":  "a pulled image is not its frozen linux/amd64 digest",
		"image-profile":   "the contained probe of the frozen Ceph image did not prove its runtime profile",
		"route-probe":     "the native SSH class inside the frozen Ceph image did not prove the verified tunnel route",
		"device-identity": "the Ceph host could not derive its devices' native identities through the image SDK",
		"runtime-profile": "the Ceph host's retained runtime profile is missing or contradicts the image it holds",
		"not-ready":       "the Ceph host preparation did not prove every phase",
	} {
		out[reason] = failure("lifecycle.unknown", message, "inspect the attempt's adapter output, then apply again; host preparation resumes from its journal")
	}
	for reason, refusal := range packageRefusals() {
		out[reason] = refusal
	}
	maps.Copy(out, sshPolicyRefusals())
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport", "remote-refusal", "adapter-exception"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph host preparation could not prove its "+reason+" boundary", "inspect the retained operation evidence before continuing")
	}
	return out
}

func packageRefusals() map[string]error {
	return map[string]error{
		"packages":            failure("lifecycle.unknown", "the Ceph host package preparation refused its request, source or result", "the adapter detail names the refusing step; check that the declared hosted RHEL DVD tree and the IBM Ceph repository serve the authored package selection to the host, then apply again"),
		"package-artifact":    failure("lifecycle.unknown", "an installed Ceph package header does not match its authenticated payload", "inspect the host's RPM database for the frozen packages; a replaced package is never adopted"),
		"package-drift":       failure("lifecycle.unknown", "the Ceph host's installed packages no longer match its completed package decision", "inspect the host's installed packages against the retained package decision; Bootwright never installs over a foreign inventory"),
		"package-host":        failure("lifecycle.unknown", "the Ceph host's /var/lib/ceph tree is not owned and protected as its packages create it", "inspect the owner and mode of /var/lib/ceph and its directories on the host; a foreign or writable tree is never adopted"),
		"package-journal":     failure("lifecycle.unknown", "the Ceph host's package journal is foreign, locked or contradicts the frozen decision", "inspect the host's package journal under /var/lib/ceph's owned markers; a foreign journal is never adopted"),
		"package-license":     failure("lifecycle.unsupported", "the IBM Ceph package transaction requires the provider license acceptance", "review the provider license and declare Entitlement.spec.license.accept"),
		"package-limit":       failure("lifecycle.unknown", "a Ceph package source served more than its bounded acquisition admits", "check that the declared package source serves the pinned repository metadata and payloads, not a larger tree"),
		"package-platform":    failure("lifecycle.unsupported", "the Ceph host does not provide the required RHEL x86_64 native package tooling", "install the Machine with the owned RHEL x86_64 DVD profile"),
		"package-timeout":     failure("lifecycle.unknown", "the Ceph package acquisition did not finish within its bounded time", "check the host's reach to the declared package source and the IBM Ceph repository, then apply again; the package journal resumes"),
		"package-resolution":  failure("lifecycle.unknown", "the frozen Ceph package set could not be resolved from the declared package sources", "check that the declared hosted RHEL DVD tree and the IBM Ceph repository serve the authored package selection to the host; the adapter detail names the refusing step"),
		"package-signature":   failure("lifecycle.unknown", "a Ceph package or publisher key is not the pinned signed artifact", "check that the package sources serve the pinned Red Hat and IBM publisher keys and payloads signed by them; an unsigned or foreign-signed package is never installed"),
		"package-transaction": failure("lifecycle.unknown", "the native Ceph package transaction did not complete", "inspect the host's dnf log for the frozen transaction, then apply again; the package journal resumes only from the exact authenticated state"),
	}
}

func hostMaterial(e lifecycle.Execution, r HostRequest, operation string, readers DependencyProofs) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	host, _ := r.target()
	key, err := provedHostKey(host, e, readers)
	if err != nil {
		return e, nil, func() {}, err
	}
	bound := maps.Clone(e.Material)
	if bound == nil {
		bound = map[string]secrets.Material{}
	}
	bound[knownHostsMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(key.Line(host.Address, host.Port))})
	cleanup := func() { bound[knownHostsMaterial].Clear(); bound[registryMaterial].Clear() }
	files := []lifecycle.MaterialFile{{Name: "fleet-key", Secret: host.SSHKeyRef, Part: secrets.PrivateKeyPart, Variable: "sshKey"}, {Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"}}
	if operation == "apply" {
		index, _ := r.source()
		files = append(files, lifecycle.MaterialFile{Name: "package-ca-" + host.Name, Secret: r.Cluster.Packages.Sources[index].CertificateRef, Part: secrets.CertificatePart, Variable: packageCertificateVariable(index)})
		repository, err := packageRepositoryTrustFile(e, r.Cluster.Packages.PackageRepositoryTrustBundleRef)
		if err != nil {
			cleanup()
			return e, nil, func() {}, err
		}
		files = append(files, repository...)
		registry, err := imageRegistryMaterial(e, r.Cluster.RegistryRef, r.Cluster.Registry)
		if err != nil {
			cleanup()
			return e, nil, func() {}, err
		}
		bound[registryMaterial] = registry
		files = append(files, lifecycle.MaterialFile{Name: "registry", Secret: registryMaterial, Part: secrets.ValuePart, Variable: "registryJSON"})
		trust, err := registryTrustFile(e, r.Cluster.RegistryTrustBundleRef)
		if err != nil {
			cleanup()
			return e, nil, func() {}, err
		}
		files = append(files, trust...)
	}
	e.Material = bound
	return e, files, cleanup, nil
}

func (c HostCapability) run(ctx context.Context, e lifecycle.Execution, operation string, r HostRequest) (lifecycle.RunResult, error) {
	if c.runner == nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the Ceph host adapter is not configured", "")
	}
	if r.Cluster.Controller == "" {
		return lifecycle.RunResult{}, failure("lifecycle.state", "Ceph host preparation requires its frozen controller identity", "")
	}
	canonical, err := r.canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	execution, files, cleanup := e, []lifecycle.MaterialFile(nil), func() {}
	if operation != "destroy" {
		execution, files, cleanup, err = hostMaterial(e, r, operation, c.proofs)
		if err != nil {
			return lifecycle.RunResult{}, err
		}
	}
	defer cleanup()
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: HostImplementation, Operation: operation, Variable: "bootwright_ceph_host", Canonical: canonical,
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Cluster.Controller}, Materials: files, Refusals: hostRefusals(), NativeErrors: true, Deadline: hostDeadline(operation)}))
}

func (c HostCapability) Apply(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	r, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	if r.Cluster.Packages, err = bindPackageSources(e, r.Cluster.Packages, c.proofs); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	host, _ := r.target()
	if err := proveClusterKey(e, r.Cluster.Security, r.Cluster.ClusterKeyRef); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	if err := proveFIPS(e, Request{Security: r.Cluster.Security, Hosts: []Host{host}}, c.proofs); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	if err := proveDisks(e, Request{Hosts: []Host{host}}, c.proofs); err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, e, "apply", r)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	outcome := map[string]reconciliation.Outcome{"changed": reconciliation.OutcomeChanged, "unchanged": reconciliation.OutcomeUnchanged}[result.Outcome]
	evidence, err := decodeHostEvidence(result.Evidence, e.Block.RequestDigest)
	if outcome == "" || err != nil || hostEffect(evidence, r) != reconciliation.EffectCompleted {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "the Ceph host preparation did not prove its postcondition", "")
	}
	return lifecycle.Result{Outcome: outcome, Evidence: result.Evidence}, nil
}

func (c HostCapability) Observe(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	r, err := c.prepare(ctx, e)
	if err != nil {
		return unknown, err
	}
	result, err := c.run(ctx, e, "observe", r)
	if err != nil {
		return unknown, err
	}
	evidence, err := decodeHostEvidence(result.Evidence, e.Block.RequestDigest)
	if err != nil {
		return unknown, err
	}
	return lifecycle.Observation{Effect: hostEffect(evidence, r), Evidence: result.Evidence}, nil
}

func (c HostCapability) Destroy(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	r, err := c.prepare(ctx, e)
	if err != nil {
		return lifecycle.Result{Outcome: reconciliation.OutcomeFailed}, err
	}
	result, err := c.run(ctx, e, "destroy", r)
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	evidence, err := decodeHostEvidence(result.Evidence, e.Block.RequestDigest)
	if err != nil || result.Outcome != "unchanged" || !retained(evidence, r) {
		return lifecycle.Result{Outcome: reconciliation.OutcomeUnknown}, failure("lifecycle.unknown", "the Ceph host release did not prove that it made no effect", "")
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeUnchanged, Evidence: result.Evidence}, nil
}

func retained(e HostEvidence, r HostRequest) bool {
	released := e
	released.Removed = false
	return e.Removed && e.identifies(r) && released.empty()
}

func (c HostCapability) ObserveRemoval(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	unknown := lifecycle.Observation{Effect: reconciliation.EffectUnknown}
	r, err := c.prepare(ctx, e)
	if err != nil {
		return unknown, err
	}
	evidence, err := json.Marshal(HostEvidence{Version: HostEvidenceVersion, Request: e.Block.RequestDigest, Host: r.Host, Owner: r.Cluster.Owner, FSID: r.Cluster.FSID, Image: r.Cluster.Image, Removed: true, Devices: []HostDevice{}})
	if err != nil {
		return unknown, failure("lifecycle.state", "the Ceph host removal evidence cannot be encoded", "")
	}
	return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
}

func (HostCapability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "host packages and the pulled image are retained shared host state; releasing them changes nothing"}, nil
}

func ProvedHost(proofs []lifecycle.BlockEvidence, cluster, machineName, image string) (HostEvidence, error) {
	var found *HostEvidence
	for _, proof := range proofs {
		if proof.Kind != string(api.StorageCluster) || proof.Object != cluster || proof.Implementation != HostImplementation || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone ||
			proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != proof.Implementation {
			continue
		}
		r, err := DecodeHostRequest(proof.Block.Request)
		if err != nil || r.Cluster.Cluster != cluster {
			return HostEvidence{}, failure("lifecycle.state", "a Ceph host preparation proof names another request", "")
		}
		host, _ := r.target()
		if host.Machine != machineName {
			continue
		}
		if found != nil {
			return HostEvidence{}, failure("lifecycle.state", "the Ceph host has ambiguous preparation proofs", "")
		}
		evidence, err := decodeHostEvidence(proof.Evidence, proof.Block.RequestDigest)
		if err != nil {
			return HostEvidence{}, failure("lifecycle.state", "the Ceph host preparation proof is unreadable", "")
		}
		r.Cluster.Owner, r.Cluster.FSID, r.Cluster.Image = evidence.Owner, evidence.FSID, image
		if evidence.Image != image || evidence.FSID != FSIDFor(evidence.Owner, cluster) || !evidence.prepared(r) {
			return HostEvidence{}, failure("lifecycle.state", "the Ceph host preparation proof does not prove the frozen image's runtime", "")
		}
		found = &evidence
	}
	if found == nil {
		return HostEvidence{}, failure("lifecycle.state", "Ceph lacks its original apply's host preparation proof", "restore the original apply evidence; each host is prepared before the cluster block runs")
	}
	return *found, nil
}
