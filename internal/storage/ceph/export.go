package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"slices"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage"
)

const ExportImplementation = "ceph-export-v1"
const ExportVersion = "ceph-export-request-v1"
const ExportEvidenceVersion = "ceph-export-evidence-v1"
const ExportOutput = "external-details"

func ExportBlockID(name string) string { return "ceph-export-" + name }
func ExportContentDigest() string {
	sum := sha256.Sum256([]byte(ExportImplementation + "\x00" + ExportVersion + "\x00" + ExportEvidenceVersion))
	return hex.EncodeToString(sum[:])
}

type ExportGateway struct {
	Addresses      []string `json:"addresses"`
	CertificateRef string   `json:"certificateRef"`
	FQDN           string   `json:"fqdn"`
	IndexPool      string   `json:"indexPool"`
	Name           string   `json:"name"`
	Port           int      `json:"port"`
	Realm          string   `json:"realm"`
	TLS            bool     `json:"tls"`
	Zone           string   `json:"zone"`
	ZoneGroup      string   `json:"zoneGroup"`
}
type ExportRequest struct {
	Cluster         string                       `json:"cluster"`
	Controller      string                       `json:"controller"`
	DefaultDataPool string                       `json:"defaultDataPool"`
	Filesystem      string                       `json:"filesystem"`
	Gateway         *ExportGateway               `json:"gateway,omitempty"`
	KeyType         string                       `json:"keyType"`
	Machines        []string                     `json:"machines"`
	MonitoringHosts []string                     `json:"monitoringHosts"`
	MonitoringPort  int                          `json:"monitoringPort"`
	Monitors        map[string]string            `json:"monitors"`
	Name            string                       `json:"name"`
	PhysicalHosts   bool                         `json:"physicalHosts"`
	Pool            string                       `json:"pool"`
	Principals      map[string]map[string]string `json:"principals"`
	Scope           json.RawMessage              `json:"scope,omitempty"`
	Version         string                       `json:"version"`
}

func (r ExportRequest) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "Ceph export") }

func (r ExportRequest) SecretReferences() []string {
	if r.Gateway == nil {
		return nil
	}
	return lifecycle.SecretNames(r.Gateway.CertificateRef)
}
func DecodeExportRequest(data []byte) (ExportRequest, error) {
	r, err := reconciliation.ThawVersion[ExportRequest](data, "Ceph export", ExportVersion)
	if err != nil {
		return r, err
	}
	if r.Cluster == "" || r.Controller == "" || !reconciliation.ValidSegment(ExportBlockID(r.Name)) || r.Filesystem == "" || r.Pool == "" || r.DefaultDataPool == "" || !strictlySorted(r.Machines) || len(r.Scope) != 0 || (r.KeyType != "aes" && r.KeyType != "aes256k") {
		return r, failure("lifecycle.state", "the frozen Ceph export request is inconsistent", "")
	}
	if len(r.Monitors) == 0 || len(r.Monitors) > 64 || !strictlySorted(r.MonitoringHosts) || r.MonitoringPort != 9283 {
		return r, failure("lifecycle.state", "Ceph export monitoring coordinates are inconsistent", "")
	}
	for name, address := range r.Monitors {
		if name == "" {
			return r, failure("lifecycle.state", "Ceph export monitor has no name", "")
		}
		if _, err := netip.ParseAddr(address); err != nil {
			return r, failure("lifecycle.state", "Ceph export monitor address is invalid", "")
		}
	}
	for _, address := range r.MonitoringHosts {
		if _, err := netip.ParseAddr(address); err != nil {
			return r, failure("lifecycle.state", "Ceph export manager address is invalid", "")
		}
	}
	if g := r.Gateway; g != nil {
		if g.Name == "" || g.FQDN == "" || g.Realm == "" || g.Zone == "" || g.ZoneGroup == "" || g.IndexPool == "" || g.Port < 1 || g.Port > 65535 || g.TLS != (g.CertificateRef != "") || !strictlySorted(g.Addresses) {
			return r, failure("lifecycle.state", "Ceph export gateway coordinates are inconsistent", "")
		}
		for _, addr := range g.Addresses {
			if _, err := netip.ParseAddr(addr); err != nil {
				return r, failure("lifecycle.state", "Ceph export gateway address is invalid", "")
			}
		}
	}
	expected, _ := json.Marshal(exportCaps(r))
	actual, _ := json.Marshal(r.Principals)
	if string(expected) != string(actual) {
		return r, failure("lifecycle.state", "the frozen Ceph export principal capabilities are inconsistent", "")
	}
	return r, nil
}
func exportCaps(r ExportRequest) map[string]map[string]string {
	health := "profile rbd-read-only"
	if g := r.Gateway; g != nil {
		health += ", allow rwx pool=" + g.Zone + ".rgw.meta, allow r pool=.rgw.root, allow rw pool=" + g.Zone + ".rgw.control, allow rx pool=" + g.Zone + ".rgw.log, allow x pool=" + g.IndexPool
	}
	return map[string]map[string]string{
		"health":   {"mon": "allow r, allow command quorum_status, allow command version", "mgr": "allow command config", "osd": health, "mds": "allow *"},
		"rbd-node": {"mon": "profile rbd, allow command 'osd blocklist'", "osd": "profile rbd pool=" + r.Pool},
		"rbd-prov": {"mon": "profile rbd, allow command 'osd blocklist'", "mgr": "allow rw", "osd": "profile rbd pool=" + r.Pool},
		"fs-node":  {"mon": "allow r, allow command 'osd blocklist'", "mgr": "allow rw", "osd": "allow rw tag cephfs *=" + r.Filesystem, "mds": "allow rw"},
		"fs-prov":  {"mon": "allow r, allow command 'osd blocklist'", "mgr": "allow rw", "osd": "allow rw tag cephfs metadata=" + r.Filesystem, "mds": "allow *"},
	}
}

type ExportCapability struct {
	runner Runner
	proofs DependencyProofs
}

var _ lifecycle.ProducedOutputProvider = ExportCapability{}

func (c ExportCapability) ProducedOutputs(block reconciliation.Block) ([]string, error) {
	r, err := DecodeExportRequest(block.Request)
	if err != nil {
		return nil, err
	}
	if block.Kind != string(api.StorageExport) || block.Object != r.Name || block.ID != ExportBlockID(r.Name) || block.Implementation != ExportImplementation {
		return nil, failure("lifecycle.state", "Ceph export output does not match its frozen block identity", "")
	}
	return []string{ExportOutput}, nil
}

func NewExport(runner Runner) ExportCapability { return ExportCapability{runner: runner} }
func (c ExportCapability) WithProofs(proofs DependencyProofs) ExportCapability {
	c.proofs = proofs
	return c
}
func (c ExportCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, failure("lifecycle.state", "Ceph export planning requires compiled state", "")
	}
	catalog := input.State.Effective()
	for _, obj := range catalog.OfKind(api.StorageExport) {
		cluster, _ := catalog.Find(api.StorageCluster, obj.Spec().Get("clusterRef").Text())
		base, err := baseRequest(catalog, cluster, input.Context.Name)
		if err != nil {
			return plan, err
		}
		refs := obj.Spec().Get("dataFoundation")
		fs, _ := catalog.Find(api.StorageFilesystem, refs.Get("filesystemRef").Text())
		data := filesystemDataPools(fs.Spec())
		if len(data) == 0 {
			return plan, failure("lifecycle.state", "Ceph export has no filesystem default data pool", "")
		}
		r := ExportRequest{Cluster: cluster.Name(), Controller: input.Controller, DefaultDataPool: nativePoolRef(catalog, data[0]), Filesystem: fs.Name(), KeyType: refs.Get("cephxKeyType").Text(), Machines: requestMachines(base), Name: obj.Name(), PhysicalHosts: physicalHosts(base.Hosts), Pool: nativePoolRef(catalog, refs.Get("rbdPoolRef").Text()), Version: ExportVersion}
		if err := exportMonitoring(catalog, cluster, &r); err != nil {
			return plan, err
		}
		requires := []reconciliation.ObjectRef{{Kind: string(api.StorageCluster), Object: r.Cluster}, {Kind: string(api.StoragePool), Object: refs.Get("rbdPoolRef").Text()}, {Kind: string(api.StorageFilesystem), Object: r.Filesystem}}
		units, err := monitoringUnits(cluster)
		if err != nil {
			return plan, err
		}
		dependencies := []string{}
		for _, unit := range units {
			dependencies = append(dependencies, ServiceUnitBlockID(r.Cluster, unit))
		}
		if name := refs.Get("objectGatewayRef").Text(); name != "" {
			gateway, _ := catalog.Find(api.StorageObjectGateway, name)
			r.Gateway, err = exportGateway(catalog, cluster, gateway, base)
			if err != nil {
				return plan, err
			}
			requires = append(requires, reconciliation.ObjectRef{Kind: string(api.StorageObjectGateway), Object: name})
			for _, dns := range catalog.OfKind(api.DNSServer) {
				requires = append(requires, reconciliation.ObjectRef{Kind: string(api.DNSServer), Object: dns.Name()})
			}
			if r.Gateway.CertificateRef != "" {
				plan.Secrets = append(plan.Secrets, r.Gateway.CertificateRef)
			}
		}
		r.Principals = exportCaps(r)
		canonical, err := r.Canonical()
		if err != nil {
			return plan, err
		}
		if _, err = DecodeExportRequest(canonical); err != nil {
			return plan, err
		}
		impacts := append([]string{"create per-export Ceph principals for " + obj.Name()}, exportGrant(r.Filesystem, refs.Get("objectGatewayRef").Text())...)
		consumes := []string{}
		if input.Verb == reconciliation.Destroy {
			impacts = []string{"retire Ceph export " + obj.Name() + " with its owned hosts"}
			consumes = []string{reconciliation.AuthorizationDataLoss}
		}
		plan.Definitions = append(plan.Definitions, reconciliation.BlockDefinition{ID: ExportBlockID(obj.Name()), Description: impacts[0], Stage: reconciliation.StageClusters, Kind: string(api.StorageExport), Object: obj.Name(), Implementation: ExportImplementation, ContentDigest: ExportContentDigest(), Request: canonical, Requires: requires, Dependencies: dependencies, Exclusive: []string{"ceph/" + r.Cluster}, Impacts: impacts, Consumes: consumes})
		for _, host := range base.Hosts {
			plan.Secrets = append(plan.Secrets, host.SSHKeyRef)
			if host.HostKeyRef != "" {
				plan.Secrets = append(plan.Secrets, host.HostKeyRef)
			}
		}
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return plan, nil
}
func (c ExportCapability) identity(ctx context.Context, e lifecycle.Execution) (ExportRequest, error) {
	if err := ctx.Err(); err != nil {
		return ExportRequest{}, err
	}
	r, err := DecodeExportRequest(e.Block.Request)
	if err != nil {
		return r, err
	}
	if e.ApplyOperation == "" || e.Block.Kind != string(api.StorageExport) || e.Block.Object != r.Name || e.Block.ID != ExportBlockID(r.Name) || e.Block.Implementation != ExportImplementation {
		return r, failure("lifecycle.state", "Ceph export does not match its original apply identity", "")
	}
	return r, nil
}
func (c ExportCapability) runExport(ctx context.Context, e lifecycle.Execution, operation string) (lifecycle.RunResult, ExportRequest, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	if err := c.ProveBinding(ctx, "", e.Block, e.Material); err != nil {
		return lifecycle.RunResult{}, r, err
	}
	scope, err := ProvedCluster(provedFor(e), r.Cluster, e.ApplyOperation)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	if !slices.Equal(r.Machines, scope.Machines) {
		return lifecycle.RunResult{}, r, failure("lifecycle.state", "Ceph export host set differs from its proved cluster", "")
	}
	if err = bindPhysicalHosts(scope.Scope.Hosts, e, c.proofs); err != nil {
		return lifecycle.RunResult{}, r, err
	}
	execution, files, cleanup, err := smbMaterial(e, SMBRequest{}, scope.Scope, operation, c.proofs)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	defer cleanup()
	if r.Gateway != nil && r.Gateway.TLS {
		files = append(files, lifecycle.MaterialFile{Name: "gateway-certificate", Secret: r.Gateway.CertificateRef, Part: secrets.CertificatePart, Variable: "gatewayCertificate"})
	}
	r.Scope, err = json.Marshal(scope.Scope)
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	canonical, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, r, err
	}
	if c.runner == nil {
		return lifecycle.RunResult{}, r, failure("lifecycle.state", "the Ceph export adapter is not configured", "")
	}
	deadline := 600 * time.Second
	if operation == "apply" {
		deadline = 1200 * time.Second
	}
	refusals := adapterRefusals()
	if r.Gateway != nil {
		refusals["gateway-dns"] = failure("lifecycle.unknown", "object gateway FQDN "+r.Gateway.FQDN+" does not resolve to its declared terminating addresses", "correct DNS resolution on the bootstrap host before continuing")
	}
	for _, reason := range []string{"export-foreign-principal", "export-key-type", "export-prerequisite", "export-identity"} {
		refusals[reason] = failure("lifecycle.unknown", "Ceph export refused: "+reason, "inspect the export prerequisites and its owned identities before continuing")
	}
	result, err := c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: ExportImplementation, Operation: operation, Variable: "bootwright_ceph_export", Canonical: canonical, Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Controller}, Materials: files, Outputs: []lifecycle.OutputFile{{Name: ExportOutput, Variable: "externalDetails"}}, Refusals: refusals, NativeErrors: true, Deadline: deadline}))
	return result, r, err
}

func (c ExportCapability) ProveBinding(ctx context.Context, contextName string, block reconciliation.Block, material map[string]secrets.Material) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r, err := DecodeExportRequest(block.Request)
	if err != nil {
		return err
	}
	if r.Gateway == nil || !r.Gateway.TLS {
		return nil
	}
	leaf, ok := boundLeaf(material[r.Gateway.CertificateRef])
	if !ok || leaf.VerifyHostname(r.Gateway.FQDN) != nil {
		return failure("secret.part", "the export gateway certificate does not prove its endpoint "+r.Gateway.FQDN, "bind a certificate naming the gateway endpoint before applying")
	}
	return nil
}

func exportMonitoring(catalog api.Catalog, cluster api.Object, r *ExportRequest) error {
	r.Monitors = map[string]string{}
	r.MonitoringHosts = []string{}
	r.MonitoringPort = 9283
	for _, service := range cluster.Spec().Get("ceph", "services").Items() {
		typ := service.Get("service_type").Text()
		if typ != "mon" && typ != "mgr" {
			continue
		}
		hosts, err := serviceHosts(cluster, service)
		if err != nil {
			return err
		}
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			name := storage.NativeHostname(node)
			if !slices.Contains(hosts, name) {
				continue
			}
			obj, _ := catalog.Find(api.Machine, node.Get("machineRef").Text())
			candidates := []string{}
			for _, raw := range machine.IPAddresses(obj) {
				addr, err := netip.ParseAddr(raw)
				if err != nil {
					continue
				}
				for _, rawNet := range cluster.Spec().Get("ceph", "networks", "publicCIDRs").Strings() {
					network, err := netip.ParsePrefix(rawNet)
					if err == nil && network.Contains(addr) {
						candidates = append(candidates, addr.Unmap().String())
						break
					}
				}
			}
			slices.Sort(candidates)
			candidates = slices.Compact(candidates)
			if len(candidates) != 1 {
				return failure("api.invariant", "Ceph export requires one unambiguous public address per monitor and manager host", "declare one address in the Ceph public networks on each selected host")
			}
			if typ == "mon" {
				r.Monitors[name] = candidates[0]
			} else {
				r.MonitoringHosts = append(r.MonitoringHosts, candidates[0])
			}
		}
	}
	slices.Sort(r.MonitoringHosts)
	r.MonitoringHosts = slices.Compact(r.MonitoringHosts)
	return nil
}

func exportGateway(catalog api.Catalog, cluster, gateway api.Object, base Request) (*ExportGateway, error) {
	gr, err := gatewayRequest(catalog, cluster, gateway, base)
	if err != nil {
		return nil, err
	}
	g := gr.Gateway
	addresses, err := storage.GatewayDNSAddresses(catalog, gateway)
	if err != nil {
		return nil, failure("api.invariant", err.Error(), "correct the gateway placement and public addresses before planning")
	}
	service, _ := storage.NativeService(cluster, gateway.Spec().Get("serviceRef").Text())
	tls := service.Get("spec", "ssl").Bool()
	port := int(positiveOr(service.Get("spec", "rgw_frontend_port"), 80))
	portDeclared := service.Has("spec", "rgw_frontend_port")
	certificate := ""
	terminating := storage.NativeServiceName(service)
	if ingress, found := frontingIngress(cluster, storage.NativeServiceName(service)); found {
		terminating = storage.NativeServiceName(ingress)
		tls = ingress.Get("spec", "ssl").Bool()
		port = int(positiveOr(ingress.Get("spec", "frontend_port"), 80))
		portDeclared = ingress.Has("spec", "frontend_port")
	}
	if tls && !portDeclared {
		port = 443
	}
	if tls {
		for _, binding := range g.Bindings {
			if binding.ServiceRef == terminating {
				certificate = binding.SecretRef
			}
		}
	}
	return &ExportGateway{Addresses: addresses, CertificateRef: certificate, FQDN: g.Endpoint, IndexPool: g.IndexPool, Name: g.Name, Port: port, Realm: g.Realm, TLS: tls, Zone: g.Zone, ZoneGroup: g.ZoneGroup}, nil
}
