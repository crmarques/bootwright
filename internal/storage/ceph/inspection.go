package ceph

import (
	"math"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage"
)

type ImageInspection struct {
	Reference string
	Digest    string
	Platform  string
}

type MonitorInspection struct {
	Host    string
	FQDN    string
	Address string
}

type DashboardURL struct {
	Host string
	URL  string
	Role string
}

type DashboardInspection struct {
	Status            string
	Reason            string
	User              string
	URLs              []DashboardURL
	CertificateSHA256 string
}

type Inspection struct {
	FSID            string
	Image           ImageInspection
	Monitors        []MonitorInspection
	Dashboard       DashboardInspection
	Specializations []string
}

const (
	DashboardReady    = "ready"
	DashboardUnproved = "unproved"
	DashboardActive   = "active"
	DashboardStandby  = "standby"
)

const (
	noClusterProof = "no apply of this context's current operation has completed the cluster's bootstrap"
	noImageProof   = "no apply of this context's current operation has recorded the cluster's resolved Ceph image"
)

func Inspect(catalog api.Catalog, cluster api.Object, proofs []lifecycle.BlockEvidence) (Inspection, string) {
	image, reason := inspectImage(cluster.Name(), proofs)
	if reason != "" {
		return Inspection{}, reason
	}
	evidence, reason := inspectCluster(cluster.Name(), proofs)
	if reason != "" {
		return Inspection{}, reason
	}
	monitors, reason := inspectMonitors(catalog, cluster, evidence.Monitors, evidence.MonAddrs)
	if reason != "" {
		return Inspection{}, reason
	}
	dashboard, reason := inspectDashboard(cluster, evidence)
	if reason != "" {
		return Inspection{}, reason
	}
	return Inspection{FSID: evidence.FSID, Image: image, Monitors: monitors, Dashboard: dashboard, Specializations: clusterSpecializations(cluster.Name(), evidence, proofs)}, ""
}

func clusterSpecializations(cluster string, evidence Evidence, proofs []lifecycle.BlockEvidence) []string {
	ids := slices.Clone(evidence.Specializations)
	for _, proof := range proofs {
		if proof.Kind != string(api.StorageCluster) || proof.Object != cluster || proof.Implementation != HostImplementation ||
			proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			continue
		}
		if host, err := decodeHostEvidence(proof.Evidence, proof.Block.RequestDigest); err == nil {
			ids = append(ids, host.Specializations...)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func inspectDashboard(cluster api.Object, evidence Evidence) (DashboardInspection, string) {
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	node := func(native string) (string, bool) {
		index := slices.IndexFunc(nodes, func(node api.Value) bool { return storage.NativeHostname(node) == native })
		if index < 0 {
			return "", false
		}
		return nodes[index].Get("name").Text(), true
	}
	d := evidence.Dashboard
	result := DashboardInspection{Status: DashboardReady, User: DashboardUser, URLs: []DashboardURL{}, CertificateSHA256: d.CertificateSHA256}
	if d.URL != "" {
		active, ok := node(evidence.MGRActive)
		if !ok {
			return DashboardInspection{}, "the active manager in the cluster's evidence names no declared node"
		}
		result.URLs = append(result.URLs, DashboardURL{Host: active, URL: d.URL, Role: DashboardActive})
		for _, standby := range evidence.MGRStandbys {
			name, ok := node(standby)
			if !ok {
				return DashboardInspection{}, "a standby manager in the cluster's evidence names no declared node"
			}
			result.URLs = append(result.URLs, DashboardURL{Host: name, URL: "https://" + standby + ":" + DashboardPort + "/", Role: DashboardStandby})
		}
	}
	switch {
	case d.URL == "" || d.CertificateSHA256 == "":
		result.Status, result.Reason = DashboardUnproved, "the apply did not observe the active manager serving the dashboard with a stored certificate"
	case !d.Reachable:
		result.Status, result.Reason = DashboardUnproved, "the apply's controller-side TLS connect to the active manager's dashboard port did not present the stored certificate"
	}
	return result, ""
}

func appliedProof(proofs []lifecycle.BlockEvidence, cluster, implementation string) (lifecycle.BlockEvidence, int) {
	var found lifecycle.BlockEvidence
	count := 0
	for _, proof := range proofs {
		if proof.Kind != string(api.StorageCluster) || proof.Object != cluster || proof.Implementation != implementation ||
			proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			continue
		}
		found = proof
		count++
	}
	return found, count
}

func inspectImage(cluster string, proofs []lifecycle.BlockEvidence) (ImageInspection, string) {
	proof, count := appliedProof(proofs, cluster, ImageImplementation)
	if count == 0 {
		return ImageInspection{}, noImageProof
	}
	if count > 1 {
		return ImageInspection{}, "the cluster's resolved Ceph image has ambiguous evidence"
	}
	evidence, err := reconciliation.DecodeEvidence[ImageEvidence](proof.Evidence, maxImageEvidence, "Ceph image")
	if err != nil || evidence.Absent || !evidence.Postcondition || evidence.Release == "" || evidence.Base == "" || evidence.Platform != PinnedPlatform || !ValidImageDigest(evidence.Digest) {
		return ImageInspection{}, "the cluster's resolved Ceph image evidence is not a resolved linux/amd64 manifest"
	}
	reference := evidence.Base + ":" + evidence.Tag
	if evidence.Tag == "" {
		reference = evidence.Base + "@" + evidence.Digest
	}
	return ImageInspection{Reference: reference, Digest: evidence.Digest, Platform: evidence.Platform}, ""
}

func inspectCluster(cluster string, proofs []lifecycle.BlockEvidence) (Evidence, string) {
	proofs = slices.DeleteFunc(slices.Clone(proofs), func(proof lifecycle.BlockEvidence) bool {
		evidence, err := decodeEvidence(proof.Evidence)
		return err == nil && (evidence.Action == "settings" || evidence.Action == "service")
	})
	proof, count := appliedProof(proofs, cluster, Implementation)
	if count == 0 {
		return Evidence{}, noClusterProof
	}
	if count > 1 {
		return Evidence{}, "the cluster's bootstrap has ambiguous evidence"
	}
	evidence, err := decodeEvidence(proof.Evidence)
	if err != nil || evidence.Version != EvidenceVersion || evidence.Action != "cluster" || !evidence.Ready || !evidence.Owned || !evidence.Exists ||
		evidence.Partial || evidence.Removed || evidence.FSID == "" || len(evidence.Monitors) == 0 {
		return Evidence{}, "the cluster's bootstrap evidence does not prove a ready cluster"
	}
	return evidence, ""
}

func inspectMonitors(catalog api.Catalog, cluster api.Object, names []string, addresses []MonitorAddress) ([]MonitorInspection, string) {
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	monitors := make([]MonitorInspection, 0, len(names))
	for _, native := range names {
		index := slices.IndexFunc(nodes, func(node api.Value) bool { return storage.NativeHostname(node) == native })
		if index < 0 {
			return nil, "a monitor in the cluster's bootstrap evidence names no declared node"
		}
		node := nodes[index]
		host, _ := catalog.Find(api.Machine, node.Get("machineRef").Text())
		ssh, ok := machine.SSH(host)
		if !ok {
			return nil, "a monitor's node declares no Machine contact address"
		}
		address := ssh.Address
		if index := slices.IndexFunc(addresses, func(m MonitorAddress) bool { return m.Host == native }); index >= 0 {
			address = addresses[index].Address
		}
		monitors = append(monitors, MonitorInspection{Host: node.Get("name").Text(), FQDN: native, Address: address})
	}
	slices.SortFunc(monitors, func(a, b MonitorInspection) int { return strings.Compare(a.Host, b.Host) })
	return monitors, ""
}

const (
	EndpointReady    = "ready"
	EndpointUnproved = "unproved"
	IssuerBound      = "bound"
	IssuerCephadm    = "cephadm"
)

const (
	noEndpointProof   = "no apply of this context's current operation has proved this endpoint"
	unreachedEndpoint = "the apply did not prove this endpoint reachable"
	unprobedNFS       = "the apply proves an NFS service's address only through the virtual IP of an ingress in front of it"
)

type EndpointInspection struct {
	Service           string
	URLs              []string
	CertificateSHA256 string
	Issuer            string
	Status            string
	Reason            string
}

type endpointSpec struct {
	FrontendPort    *int64
	KeepaliveOnly   bool
	Port            *int64
	RGWFrontendPort *int64
	SSL             bool
	VirtualIP       string
}

type endpointDocument struct {
	Placement   nativePlacement
	ServiceID   string
	ServiceType string
	Spec        endpointSpec
}

type endpointCandidate struct {
	row      EndpointInspection
	proved   func(Evidence) bool
	digest   bool
	unprobed bool
}

func specNumber(spec map[string]any, key string) *int64 {
	number, ok := spec[key].(float64)
	if !ok || number != math.Trunc(number) {
		return nil
	}
	value := int64(number)
	return &value
}

func specFlag(spec map[string]any, key string) bool {
	flag, _ := spec[key].(bool)
	return flag
}

func endpointDocuments(r Request) (map[string]endpointDocument, bool) {
	raw := unitDocuments(r)
	if raw == nil {
		return nil, false
	}
	documents := map[string]endpointDocument{}
	for _, document := range raw {
		spec, _ := document.Spec.(map[string]any)
		address, _ := spec["virtual_ip"].(string)
		documents[document.ServiceType] = endpointDocument{
			Placement: document.Placement, ServiceID: document.ServiceID, ServiceType: document.ServiceType,
			Spec: endpointSpec{FrontendPort: specNumber(spec, "frontend_port"), KeepaliveOnly: specFlag(spec, "keepalive_only"), Port: specNumber(spec, "port"),
				RGWFrontendPort: specNumber(spec, "rgw_frontend_port"), SSL: specFlag(spec, "ssl"), VirtualIP: address},
		}
	}
	return documents, true
}

func EndpointGateways(cluster string, frozen []reconciliation.Block) []string {
	names := []string{}
	for _, block := range frozen {
		if block.Kind != string(api.StorageObjectGateway) || block.Implementation != Implementation {
			continue
		}
		if r, err := DecodeRequest(block.Request); err == nil && r.Action == "gateway" && r.Gateway != nil && r.Cluster == cluster {
			names = append(names, block.Object)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

func InspectEndpoints(cluster string, frozen []reconciliation.Block, proofs []lifecycle.BlockEvidence) []EndpointInspection {
	rows := []EndpointInspection{}
	for _, block := range frozen {
		if block.Implementation != Implementation {
			continue
		}
		r, err := DecodeRequest(block.Request)
		if err != nil {
			continue
		}
		candidate, ok := endpointCandidateOf(cluster, block, r)
		if !ok || len(candidate.row.URLs) == 0 {
			continue
		}
		rows = append(rows, candidate.settle(block, r, proofs))
	}
	slices.SortStableFunc(rows, func(a, b EndpointInspection) int { return strings.Compare(a.Service, b.Service) })
	return rows
}

func endpointCandidateOf(cluster string, block reconciliation.Block, r Request) (endpointCandidate, bool) {
	unit := block.Kind == string(api.StorageCluster) && block.Object == cluster && r.Action == "service" && r.Service != nil
	switch {
	case unit && r.Service.Type == "mgmt-gateway":
		return mgmtEndpoint(r)
	case unit && r.Service.Type == "nfs":
		return nfsEndpoint(r)
	case block.Kind == string(api.StorageObjectGateway) && r.Action == "gateway" && r.Gateway != nil && r.Cluster == cluster:
		return gatewayEndpoint(r)
	}
	return endpointCandidate{}, false
}

func (c endpointCandidate) settle(block reconciliation.Block, r Request, proofs []lifecycle.BlockEvidence) EndpointInspection {
	row := c.row
	row.Status = EndpointUnproved
	if c.unprobed {
		row.Reason = unprobedNFS
		return row
	}
	evidence, found := endpointProof(block, r, proofs)
	switch {
	case !found:
		row.Reason = noEndpointProof
	case !c.proved(evidence):
		row.Reason = unreachedEndpoint
	default:
		row.Status = EndpointReady
		if c.digest {
			row.CertificateSHA256 = evidence.Endpoint.CertificateSHA256
		}
	}
	return row
}

func endpointProof(block reconciliation.Block, r Request, proofs []lifecycle.BlockEvidence) (Evidence, bool) {
	var found Evidence
	count := 0
	for _, proof := range proofs {
		if proof.Kind != block.Kind || proof.Object != block.Object || proof.Implementation != block.Implementation ||
			proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			continue
		}
		evidence, err := decodeEvidence(proof.Evidence)
		if err != nil || evidence.Version != EvidenceVersion || evidence.Request != block.RequestDigest || evidence.Action != r.Action {
			continue
		}
		found = evidence
		count++
	}
	return found, count == 1
}

func endpointURLs(scheme string, hosts []string, port int64) []string {
	urls := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		urls = append(urls, scheme+"://"+host+":"+strconv.FormatInt(port, 10)+"/")
	}
	return urls
}

func bareAddress(address string) string {
	bare, _, _ := strings.Cut(address, "/")
	return bare
}

func sortedHosts(document endpointDocument) []string {
	return slices.Sorted(slices.Values(document.Placement.Hosts))
}

func mgmtEndpoint(r Request) (endpointCandidate, bool) {
	unit := r.Service
	documents, ok := endpointDocuments(r)
	gateway, found := documents["mgmt-gateway"]
	if !ok || !found || gateway.Spec.Port == nil {
		return endpointCandidate{}, false
	}
	hosts := sortedHosts(gateway)
	if gateway.Spec.VirtualIP != "" {
		hosts = []string{gateway.Spec.VirtualIP}
	}
	issuer := IssuerCephadm
	if len(unit.Bindings) != 0 {
		issuer = IssuerBound
	}
	return endpointCandidate{
		row:    EndpointInspection{Service: "mgmt-gateway", URLs: endpointURLs("https", hosts, *gateway.Spec.Port), Issuer: issuer},
		proved: func(e Evidence) bool { return e.Ready && e.Endpoint.Reachable }, digest: true,
	}, true
}

func nfsEndpoint(r Request) (endpointCandidate, bool) {
	unit := r.Service
	documents, ok := endpointDocuments(r)
	nfs, found := documents["nfs"]
	if !ok || !found {
		return endpointCandidate{}, false
	}
	row := EndpointInspection{Service: "nfs." + unit.ID}
	port := int64(2049)
	if nfs.Spec.Port != nil {
		port = *nfs.Spec.Port
	}
	ingress, fronted := documents["ingress"]
	if !fronted {
		hosts := sortedHosts(nfs)
		if nfs.Spec.VirtualIP != "" {
			hosts = []string{bareAddress(nfs.Spec.VirtualIP)}
		}
		row.URLs = endpointURLs("nfs", hosts, port)
		return endpointCandidate{row: row, unprobed: true}, true
	}
	if !ingress.Spec.KeepaliveOnly {
		port = 2049
		if ingress.Spec.FrontendPort != nil {
			port = *ingress.Spec.FrontendPort
		}
	}
	row.URLs = endpointURLs("nfs", []string{bareAddress(ingress.Spec.VirtualIP)}, port)
	return endpointCandidate{row: row, proved: func(e Evidence) bool { return e.Ready }}, true
}

func gatewayEndpoint(r Request) (endpointCandidate, bool) {
	gateway := r.Gateway
	documents, ok := endpointDocuments(r)
	rgw, found := documents["rgw"]
	if !ok || !found {
		return endpointCandidate{}, false
	}
	ingress, fronted := documents["ingress"]
	terminating := rgw
	if fronted {
		terminating = ingress
	}
	scheme, port := "http", int64(80)
	if terminating.Spec.SSL {
		scheme, port = "https", 443
	}
	switch {
	case fronted && ingress.Spec.FrontendPort != nil:
		port = *ingress.Spec.FrontendPort
	case rgw.Spec.RGWFrontendPort != nil:
		port = *rgw.Spec.RGWFrontendPort
	}
	hosts := sortedHosts(rgw)
	switch {
	case gateway.Endpoint != "":
		hosts = []string{gateway.Endpoint}
	case fronted:
		hosts = []string{bareAddress(ingress.Spec.VirtualIP)}
	}
	issuer := ""
	if scheme == "https" {
		issuer = IssuerCephadm
		if len(gateway.Bindings) != 0 {
			issuer = IssuerBound
		}
	}
	return endpointCandidate{
		row:    EndpointInspection{Service: "rgw." + rgw.ServiceID, URLs: endpointURLs(scheme, hosts, port), Issuer: issuer},
		proved: func(e Evidence) bool { return e.Ready && e.Endpoint.Reachable }, digest: scheme == "https",
	}, true
}

var scopeRows = map[api.Kind]func(api.Catalog, string, []reconciliation.Block) string{
	api.StoragePlacementPolicy: frozenScope(api.StoragePlacementPolicy, "policy", policyScopeText),
	api.StoragePool:            frozenScope(api.StoragePool, "pool", poolScopeText),
	api.StorageRBDImage:        frozenScope(api.StorageRBDImage, "rbd", rbdScopeText),
	api.StorageClient:          frozenScope(api.StorageClient, "client", clientScopeText),
	api.StorageFilesystem:      frozenScope(api.StorageFilesystem, "filesystem", filesystemScopeText),
	api.StorageSubvolume:       frozenScope(api.StorageSubvolume, "subvolume", subvolumeScopeText),
	api.StorageObjectGateway:   frozenScope(api.StorageObjectGateway, "gateway", gatewayScopeText),
	api.StorageObjectUser:      frozenScope(api.StorageObjectUser, "object-user", objectUserScopeText),
	api.StorageNFSExport:       frozenScope(api.StorageNFSExport, "nfs-export", nfsExportScopeText),
	api.StorageSMBResource:     smbFrozenScope,
	api.StorageExport:          declaredScope(api.StorageExport, exportScopeText),
}

func smbFrozenScope(_ api.Catalog, name string, frozen []reconciliation.Block) string {
	var matched []SMBRequest
	for _, block := range frozen {
		if block.Kind != string(api.StorageSMBResource) || block.Object != name || block.Implementation != SMBImplementation {
			continue
		}
		if r, err := DecodeSMBRequest(block.Request); err == nil {
			matched = append(matched, r)
		}
	}
	if len(matched) != 1 {
		return ""
	}
	return smbScopeText(matched[0])
}

func smbScopeText(r SMBRequest) string {
	switch r.ResourceType {
	case api.SMBCluster:
		return "smb cluster " + r.ID + ", " + r.nativeLeaf("auth_mode")
	case api.SMBShare:
		return "share " + r.ID + " on smb cluster " + r.nativeLeaf("cluster_id")
	case api.SMBJoinAuth:
		return "join auth " + r.ID
	case api.SMBUsersGroups:
		return "users and groups " + r.ID
	}
	return ""
}

func ResourceScope(catalog api.Catalog, kind api.Kind, name string, frozen []reconciliation.Block) string {
	row, found := scopeRows[kind]
	if !found {
		return ""
	}
	return row(catalog, name, frozen)
}

func frozenScope(kind api.Kind, action string, render func(Request) string) func(api.Catalog, string, []reconciliation.Block) string {
	return func(_ api.Catalog, name string, frozen []reconciliation.Block) string {
		var matched []Request
		for _, block := range frozen {
			if block.Kind != string(kind) || block.Object != name || block.Implementation != Implementation {
				continue
			}
			if r, err := DecodeRequest(block.Request); err == nil && r.Action == action {
				matched = append(matched, r)
			}
		}
		if len(matched) != 1 {
			return ""
		}
		return render(matched[0])
	}
}

func declaredScope(kind api.Kind, render func(api.Value) string) func(api.Catalog, string, []reconciliation.Block) string {
	return func(catalog api.Catalog, name string, _ []reconciliation.Block) string {
		object, found := catalog.Find(kind, name)
		if !found {
			return ""
		}
		return render(object.Spec())
	}
}

func gatewayScopeText(r Request) string {
	if r.Gateway == nil {
		return ""
	}
	return "zone " + r.Gateway.Realm + "/" + r.Gateway.ZoneGroup + "/" + r.Gateway.Zone
}

func objectUserScopeText(r Request) string {
	if r.ObjectUser == nil {
		return ""
	}
	return "user " + r.ObjectUser.Name + " in zone " + r.ObjectUser.Realm + "/" + r.ObjectUser.ZoneGroup + "/" + r.ObjectUser.Zone
}

func nfsExportScopeText(r Request) string {
	if r.NFSExport == nil {
		return ""
	}
	return "export " + r.NFSExport.Native.Pseudo + " on nfs." + r.NFSExport.Service
}

func policyScopeText(r Request) string {
	if r.Policy == nil {
		return ""
	}
	scope := "rule " + r.Policy.Rule + ", failure domain " + r.Policy.FailureDomain
	if r.Policy.DeviceClass != "" {
		scope += ", device class " + r.Policy.DeviceClass
	}
	return scope
}

func poolScopeText(r Request) string {
	p := r.Pool
	if p == nil {
		return ""
	}
	if p.Erasure == nil {
		return "replicated size " + strconv.Itoa(p.Size) + "/min " + strconv.Itoa(p.MinSize) + ", application " + p.Application
	}
	scope := "erasure k=" + strconv.Itoa(p.Erasure.DataChunks) + " m=" + strconv.Itoa(p.Erasure.CodingChunks) + ", application " + p.Application
	if p.Overwrites {
		scope += ", overwrites"
	}
	return scope
}

func rbdScopeText(r Request) string {
	if r.RBD == nil {
		return ""
	}
	scope := "image " + r.RBD.Pool + "/" + r.RBD.Name + ", " + strconv.FormatInt(r.RBD.SizeBytes, 10) + " bytes"
	if r.RBD.DataPool != "" {
		scope += ", data pool " + r.RBD.DataPool
	}
	return scope
}

func clientScopeText(r Request) string {
	if r.Client == nil {
		return ""
	}
	prefix := "client." + r.Client.Name + " " + r.Client.Access + " on "
	if r.Client.Subvolume != "" {
		group := ""
		if r.Client.Group != "" {
			group = " in group " + r.Client.Group
		}
		return prefix + "subvolume " + r.Client.Subvolume + group + " of filesystem " + r.Client.Filesystem
	}
	if r.Client.Filesystem != "" {
		return prefix + "filesystem " + r.Client.Filesystem
	}
	scope := prefix + "pool " + r.Client.Pool
	if len(r.Client.DataPools) != 0 {
		scope += ", data pools " + strings.Join(r.Client.DataPools, " ")
	}
	return scope
}

func filesystemScopeText(r Request) string {
	if r.Filesystem == nil {
		return ""
	}
	return "filesystem " + r.Filesystem.Name + ", metadata " + r.Filesystem.MetadataPool + ", data " + strings.Join(r.Filesystem.DataPools, " ")
}

func subvolumeScopeText(r Request) string {
	if r.Subvolume == nil {
		return ""
	}
	group := r.Subvolume.Group
	if group == "" {
		group = "_nogroup"
	}
	return "subvolume " + r.Subvolume.Filesystem + "/" + group + "/" + r.Subvolume.Name
}

func exportScopeText(spec api.Value) string {
	foundation := spec.Get("dataFoundation")
	if !foundation.Present() {
		return "export"
	}
	scope := "export of pool " + foundation.Get("rbdPoolRef").Text() + ", filesystem " + foundation.Get("filesystemRef").Text()
	if gateway := foundation.Get("objectGatewayRef").Text(); gateway != "" {
		scope += ", object gateway " + gateway
	}
	return scope
}
