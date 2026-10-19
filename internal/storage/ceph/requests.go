package ceph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage"
	"github.com/crmarques/bootwright/internal/substrate"
)

const Version = "ceph-core-v14"
const Implementation = "storage-ceph-core-v3"

type Device struct {
	Path      string `json:"path"`
	Serial    string `json:"serial"`
	SizeBytes int64  `json:"sizeBytes"`
}

type Host struct {
	Address    string            `json:"address"`
	Devices    []Device          `json:"devices"`
	HostKeyRef string            `json:"hostKeyRef,omitempty"`
	KnownHosts string            `json:"knownHosts,omitempty"`
	Labels     []string          `json:"labels"`
	Location   map[string]string `json:"location"`
	Machine    string            `json:"machine"`
	Name       string            `json:"name"`
	Port       int               `json:"port"`
	SSHKeyRef  string            `json:"sshKeyRef"`
	User       string            `json:"user"`
}

type Tool struct {
	PackageVersion string `json:"packageVersion"`
	Path           string `json:"path"`
}

type Policy struct {
	DeviceClass   string `json:"deviceClass"`
	FailureDomain string `json:"failureDomain"`
	Name          string `json:"name"`
	Root          string `json:"root"`
	Rule          string `json:"rule"`
	Size          int    `json:"size"`
}

type PoolAutoscale struct {
	Bulk        bool   `json:"bulk"`
	MaxPGs      int    `json:"maxPGs"`
	MinPGs      int    `json:"minPGs"`
	Mode        string `json:"mode"`
	TargetBytes int64  `json:"targetBytes"`
	TargetRatio string `json:"targetRatio"`
}

type PoolCompression struct {
	Algorithm     string `json:"algorithm"`
	MaxBlob       int64  `json:"maxBlob"`
	MinBlob       int64  `json:"minBlob"`
	Mode          string `json:"mode"`
	RequiredRatio string `json:"requiredRatio"`
}

type PoolErasure struct {
	CodingChunks    int               `json:"codingChunks"`
	DataChunks      int               `json:"dataChunks"`
	DeviceClass     string            `json:"deviceClass"`
	FailureDomain   string            `json:"failureDomain"`
	Plugin          string            `json:"plugin"`
	Profile         string            `json:"profile"`
	ProfileDefaults map[string]string `json:"profileDefaults"`
	Root            string            `json:"root"`
	StripeUnit      int               `json:"stripeUnit"`
	Technique       string            `json:"technique"`
}

type PoolQuota struct {
	MaxBytes   int64 `json:"maxBytes"`
	MaxObjects int64 `json:"maxObjects"`
}

type Pool struct {
	Application string          `json:"application"`
	Autoscale   PoolAutoscale   `json:"autoscale"`
	Compression PoolCompression `json:"compression"`
	CrushRule   string          `json:"crushRule"`
	Erasure     *PoolErasure    `json:"erasure,omitempty"`
	Filesystem  string          `json:"filesystem"`
	MinSize     int             `json:"minSize"`
	Name        string          `json:"name"`
	Overwrites  bool            `json:"overwrites"`
	PGNum       int             `json:"pgNum"`
	Quota       PoolQuota       `json:"quota"`
	Role        string          `json:"role"`
	Size        int             `json:"size"`
	Type        string          `json:"type"`
}

type Client struct {
	Access string `json:"access"`
	Name   string `json:"name"`
	Pool   string `json:"pool"`
}

type RBDImage struct {
	DataPool  string   `json:"dataPool"`
	Features  []string `json:"features"`
	Name      string   `json:"name"`
	Pool      string   `json:"pool"`
	SizeBytes int64    `json:"sizeBytes"`
}

type FilesystemGroup struct {
	GID       int    `json:"gid"`
	Mode      string `json:"mode"`
	Name      string `json:"name"`
	Pool      string `json:"pool"`
	SizeBytes int64  `json:"sizeBytes"`
	UID       int    `json:"uid"`
}

type Filesystem struct {
	ActiveCount        int               `json:"activeCount"`
	DataPools          []string          `json:"dataPools"`
	DefaultDataPool    string            `json:"defaultDataPool"`
	Groups             []FilesystemGroup `json:"groups"`
	MDSService         string            `json:"mdsService"`
	MetadataPool       string            `json:"metadataPool"`
	Name               string            `json:"name"`
	StandbyCountWanted int               `json:"standbyCountWanted"`
	StandbyReplay      bool              `json:"standbyReplay"`
}

type Subvolume struct {
	Filesystem string `json:"filesystem"`
	GID        int    `json:"gid"`
	Group      string `json:"group"`
	Mode       string `json:"mode"`
	Name       string `json:"name"`
	Pool       string `json:"pool"`
	SizeBytes  int64  `json:"sizeBytes"`
	UID        int    `json:"uid"`
}

type nativePlacement struct {
	Count        *int64   `json:"count,omitempty"`
	CountPerHost *int64   `json:"count_per_host,omitempty"`
	Hosts        []string `json:"hosts"`
}
type nativeDevices struct {
	Paths []string `json:"paths"`
}
type nativeOSD struct {
	DataDevices nativeDevices `json:"data_devices"`
}
type nativeService struct {
	Placement   nativePlacement `json:"placement"`
	ServiceID   string          `json:"service_id,omitempty"`
	ServiceType string          `json:"service_type"`
	Spec        *nativeOSD      `json:"spec,omitempty"`
}

const DefaultPackageRepository = "https://public.dhe.ibm.com/ibmdl/export/pub/storage/ceph/9/rhel9/x86_64"

type DelegatedService struct {
	Hosts []string `json:"hosts"`
	Name  string   `json:"name"`
}

type AuxiliaryReference struct {
	Image string `json:"image"`
	Name  string `json:"name"`
}

type Request struct {
	Action                   string                       `json:"action"`
	AnsiblePackageVersion    string                       `json:"ansiblePackageVersion"`
	Auxiliary                []AuxiliaryReference         `json:"auxiliary,omitempty"`
	Bootstrap                string                       `json:"bootstrap"`
	CallHome                 string                       `json:"callHome"`
	Cephadm                  Tool                         `json:"cephadm"`
	Client                   *Client                      `json:"client,omitempty"`
	Cluster                  string                       `json:"cluster"`
	ClusterKeyRef            string                       `json:"clusterKeyRef"`
	ClusterNetworks          []string                     `json:"clusterNetworks"`
	ClusterSSHTransport      string                       `json:"clusterSSHTransport"`
	ClusterSSHUser           string                       `json:"clusterSSHUser"`
	Config                   map[string]map[string]string `json:"config,omitempty"`
	Controller               string                       `json:"controller"`
	Delegated                []DelegatedService           `json:"delegated,omitempty"`
	Filesystem               *Filesystem                  `json:"filesystem,omitempty"`
	FSID                     string                       `json:"fsid,omitempty"`
	Hosts                    []Host                       `json:"hosts"`
	Image                    string                       `json:"image"`
	MonIP                    string                       `json:"monIP"`
	NFSGaneshaPackageVersion string                       `json:"nfsGaneshaPackageVersion,omitempty"`
	Owner                    string                       `json:"owner,omitempty"`
	PackageRepository        string                       `json:"packageRepository"`
	Packages                 *PackageRequest              `json:"packages,omitempty"`
	Policy                   *Policy                      `json:"policy,omitempty"`
	Pool                     *Pool                        `json:"pool,omitempty"`
	PublicNetworks           []string                     `json:"publicNetworks"`
	RBD                      *RBDImage                    `json:"rbd,omitempty"`
	Registry                 string                       `json:"registry"`
	RegistryRef              string                       `json:"registryRef"`
	Release                  string                       `json:"release"`
	Service                  *ServiceUnit                 `json:"service,omitempty"`
	Services                 []json.RawMessage            `json:"services"`
	Settings                 *Settings                    `json:"settings,omitempty"`
	Subvolume                *Subvolume                   `json:"subvolume,omitempty"`
	Version                  string                       `json:"version"`
}

func (r Request) Canonical() ([]byte, error) { return reconciliation.Freeze(r, "Ceph") }
func DecodeRequest(data []byte) (Request, error) {
	return reconciliation.ThawVersion[Request](data, "Ceph", Version)
}

func scopeOf(r Request) ClusterScope {
	return ClusterScope{AnsiblePackageVersion: r.AnsiblePackageVersion, Auxiliary: r.Auxiliary, Bootstrap: r.Bootstrap, CallHome: r.CallHome, Cephadm: r.Cephadm, Cluster: r.Cluster,
		ClusterKeyRef: r.ClusterKeyRef, ClusterNetworks: r.ClusterNetworks, ClusterSSHTransport: r.ClusterSSHTransport, ClusterSSHUser: r.ClusterSSHUser,
		Config: r.Config, Controller: r.Controller, FSID: r.FSID, Hosts: r.Hosts, Image: r.Image, MonIP: r.MonIP,
		NFSGaneshaPackageVersion: r.NFSGaneshaPackageVersion, Owner: r.Owner, PackageRepository: r.PackageRepository, Packages: r.Packages,
		PublicNetworks: r.PublicNetworks, Registry: r.Registry, RegistryRef: r.RegistryRef, Release: r.Release, Services: r.Services, Version: r.Version}
}

func FSIDFor(operation, cluster string) string {
	d := sha256.Sum256([]byte("bootwright.ceph.incarnation-v1\x00" + operation + "\x00" + cluster))
	d[6] = (d[6] & 15) | 0x50
	d[8] = (d[8] & 63) | 0x80
	x := hex.EncodeToString(d[:16])
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}

func baseRequest(c api.Catalog, cluster api.Object, contextName string) (Request, error) {
	spec := cluster.Spec().Get("ceph")
	if spec.Get("distribution").Text() != "ibm" {
		return Request{}, failure("lifecycle.unsupported", "core Ceph realizes only the IBM Storage Ceph distribution", "set ceph.distribution to ibm")
	}
	base, imageVersion := spec.Get("image", "base").Text(), spec.Get("image", "version").Text()
	reference := base + ":" + imageVersion
	if ValidImageDigest(imageVersion) {
		reference = base + "@" + imageVersion
	}
	ent, _ := c.Find(api.Entitlement, spec.Get("entitlementRef").Text())
	if !ent.Spec().Get("license", "accept").Bool() {
		return Request{}, failure("lifecycle.unsupported", "the selected IBM license has not been accepted", "review the provider license and set license.accept on the declared Entitlement")
	}
	if registry := ent.Spec().Get("registry", "url").Text(); base != registry && !strings.HasPrefix(base, registry+"/") {
		return Request{}, failure("lifecycle.unsupported", "the Entitlement registry does not match the selected Ceph image registry", "set the Entitlement registry.url to the selected image registry")
	}
	registry, _, _ := strings.Cut(ent.Spec().Get("registry", "url").Text(), "/")
	if !strings.HasPrefix(base, registry+"/") {
		return Request{}, failure("lifecycle.unsupported", "the Ceph image base names only the Entitlement registry host, not a repository under it", "set ceph.image.base to a repository under the Entitlement registry, such as "+registry+"/<repository>")
	}
	r := Request{Version: Version, Action: "cluster", Cluster: cluster.Name(), Image: reference,
		Cephadm: Tool{Path: "/usr/sbin/cephadm", PackageVersion: spec.Get("packageVersion").Text()}, Release: spec.Get("release").Text(), AnsiblePackageVersion: spec.Get("cephadm", "ansible", "packageVersion").Text(), NFSGaneshaPackageVersion: spec.Get("nfsGanesha", "packageVersion").Text(),
		ClusterSSHUser: spec.Get("cephadm", "clusterSSH", "user").Text(), ClusterKeyRef: spec.Get("cephadm", "clusterSSH", "keyRef").Text(),
		RegistryRef: ent.Spec().Get("registry", "credentialsRef").Text(), Registry: registry, CallHome: spec.Get("ibm", "callHome").Text(),
		PublicNetworks: spec.Get("networks", "publicCIDRs").Strings(), ClusterNetworks: spec.Get("networks", "clusterCIDRs").Strings(), Hosts: []Host{}, Services: []json.RawMessage{}}
	r.PackageRepository = spec.Get("packageRepository").Text()
	if r.PackageRepository == "" {
		r.PackageRepository = DefaultPackageRepository
	}
	r.ClusterSSHTransport = spec.Get("cephadm", "clusterSSH", "transport").Text()
	if r.ClusterSSHTransport == "" {
		r.ClusterSSHTransport = "native"
	}
	if r.ClusterSSHTransport != "native" && r.ClusterSSHTransport != "verified-tunnel" {
		return Request{}, failure("api.invariant", "unknown Ceph SSH transport", "select native or verified-tunnel; each transport requires artifact qualification")
	}
	if r.ClusterSSHUser == "" || r.ClusterKeyRef == "" || r.RegistryRef == "" || spec.Has("topology", "stretch") || spec.Get("security", "fips", "enabled").Bool() || spec.Get("security", "cephx", "keyType").Present() && spec.Get("security", "cephx", "keyType").Text() != "aes" {
		return Request{}, failure("lifecycle.unsupported", "the requested Ceph trust or topology profile is not implemented by the core adapter", "declare the cluster SSH key, registry entitlement and a non-stretch CephX core profile")
	}
	if len(r.PublicNetworks) == 0 {
		return Request{}, failure("api.required", "core Ceph requires an explicit public network", "declare ceph.networks.publicCIDRs")
	}
	config, configIssues := storage.CoreCephConfig(spec)
	if len(configIssues) != 0 {
		issue := configIssues[0]
		return Request{}, failure(issue.Code, issue.Message, issue.Remediation)
	}
	r.Config = config
	if spec.Get("mgrModules").Len() != 0 || spec.Get("cephadm", "workarounds").Len() != 0 || spec.Get("cephadm", "bootstrap", "singleHostDefaults").Bool() {
		return Request{}, failure("lifecycle.unsupported", "the core adapter does not yet realize additional cluster configuration", "use only the qualified core MON/MGR/OSD profile")
	}
	for _, binding := range spec.Get("serviceBindings").Items() {
		if ref := binding.Get("serviceRef").Text(); !gatewayExecuted(c, cluster, ref) {
			return Request{}, failure("lifecycle.unsupported", "Bootwright does not realize the service binding of "+ref+"; it realizes only the TLS bindings of an rgw service that a StorageObjectGateway binds and of the ingress that fronts it", "remove the spec.ceph.serviceBindings entry for "+ref)
		}
	}
	if err := bindHosts(&r, c, cluster, contextName); err != nil {
		return Request{}, err
	}
	if r.ClusterSSHTransport == "verified-tunnel" {
		seen := map[string]bool{}
		for _, host := range r.Hosts {
			address, err := netip.ParseAddr(host.Address)
			if err != nil || address.String() != host.Address || address.Zone() != "" || address.IsUnspecified() || address.IsMulticast() || seen[host.Address] {
				return Request{}, failure("lifecycle.unsupported", "verified Ceph transport requires distinct canonical IP endpoints", "declare one distinct canonical management IP per Ceph host")
			}
			seen[host.Address] = true
		}
	}
	if err := projectServices(&r, cluster); err != nil {
		return Request{}, err
	}

	selectDevices(&r)
	return r, nil
}

func bindHosts(r *Request, c api.Catalog, cluster api.Object, contextName string) error {
	spec := cluster.Spec().Get("ceph")
	if reason, remedy, gap := replicationNetworkGap(c, cluster); gap {
		return failure("api.invariant", reason, remedy)
	}
	for _, node := range spec.Get("topology", "nodes").Items() {
		m, _ := c.Find(api.Machine, node.Get("machineRef").Text())
		ssh, ok := machine.SSH(m)
		provider, _ := c.Find(api.InfraProvider, m.Spec().Get("substrate", "providerRef").Text())
		if !ok || !machine.Installed(m) || !provider.Spec().Has("libvirt") || ssh.PrivateKeyRef == "" || ssh.PasswordRef != "" || ssh.OperatorIdentity || ssh.User != r.ClusterSSHUser || ssh.PrivateKeyRef == r.ClusterKeyRef {
			return failure("lifecycle.unsupported", "core Ceph requires installed libvirt guests and separate fleet and cluster SSH keys", "use the three-node libvirt Ceph example and distinct fleet and orchestration identities")
		}
		if len(r.Hosts) > 0 && ssh.PrivateKeyRef != r.Hosts[0].SSHKeyRef {
			return failure("lifecycle.unsupported", "core Ceph currently requires one fleet SSH key for its guests", "declare a shared fleet identity distinct from the native cluster identity")
		}
		name := storage.NativeHostname(node)
		fqdn := ""
		for _, a := range m.Spec().Get("network", "addresses").Items() {
			if a.Get("name").Text() == "fqdn" {
				fqdn = a.Get("address").Text()
			}
		}
		if fqdn != "" && name != fqdn {
			return failure("api.invariant", "native Ceph hostname differs from the installed Machine hostname", "set topology.nodes[].fqdn to the Machine's declared fqdn")
		}
		hostKeyRef := m.Spec().Get("os", "install", "hostKeyRef").Text()
		if ssh.KnownHostsRef != "" {
			return failure("lifecycle.unsupported", "core Ceph requires its owned managed installation's verified host key", "use captured installation evidence; do not substitute an unrelated knownHostsRef")
		}
		h := Host{Name: name, Machine: m.Name(), Address: ssh.Address, User: ssh.User, Port: ssh.Port, Devices: []Device{}, Labels: node.Get("labels").Strings(), Location: map[string]string{}, SSHKeyRef: ssh.PrivateKeyRef, HostKeyRef: hostKeyRef}
		for _, loc := range node.Get("location").Fields() {
			h.Location[loc.Name] = loc.Value.Text()
		}
		if len(h.Location) != 0 {
			return failure("lifecycle.unsupported", "native host CRUSH locations require release qualification", "omit topology.nodes[].location for the core virtual profile")
		}
		profile := api.Value{}
		for _, p := range provider.Spec().Get("libvirt", "machineProfiles").Items() {
			if p.Get("name").Text() == m.Spec().Get("substrate", "profileRef").Text() {
				profile = p
			}
		}
		dataDisks := profile.Get("dataDisks").Items()
		presentations, valid := substrate.DataDiskPresentations(dataDisks)
		if !valid {
			return failure("lifecycle.unsupported", "unsupported virtual data disk media or targets", "use the supported libvirt profile disk bound and media")
		}
		for i, disk := range dataDisks {
			size, _ := disk.Get("sizeGiB").Int64()
			h.Devices = append(h.Devices, Device{Path: "/dev/" + presentations[i].Target, Serial: substrate.DataDiskSerial(contextName, m.Name(), disk.Get("name").Text()), SizeBytes: size << 30})
		}
		r.Hosts = append(r.Hosts, h)
		addressRef := spec.Get("cephadm", "addressRef").Text()
		if node.Get("name").Text() == spec.Get("cephadm", "bootstrap", "node").Text() {
			if v := spec.Get("cephadm", "bootstrap", "addressRef").Text(); v != "" {
				addressRef = v
			}
			r.Bootstrap = name
			r.MonIP = ssh.Address
			if addressRef != "" {
				var err error
				r.MonIP, err = machine.ResolveAddress(m, addressRef)
				if err != nil {
					return err
				}
				if r.MonIP != ssh.Address {
					return failure("lifecycle.unsupported", "a separate bootstrap address is not yet modeled", "select the same declared management address for bootstrap")
				}
			}
		} else if addressRef != "" {
			address, err := machine.ResolveAddress(m, addressRef)
			if err != nil {
				return err
			}
			if address != ssh.Address {
				return failure("lifecycle.unsupported", "separate orchestration and fleet addresses are not yet modeled", "use the same declared management address until separate host enrollment addresses are implemented")
			}
		}
	}
	slices.SortFunc(r.Hosts, func(a, b Host) int { return strings.Compare(a.Name, b.Name) })
	if len(r.Hosts) < 3 || r.Bootstrap == "" {
		return failure("lifecycle.unsupported", "the core IBM profile requires at least three owned hosts and a bootstrap host", "declare a provider-supported topology with at least three hosts")
	}
	return nil
}

func replicationNetworkGap(c api.Catalog, cluster api.Object) (string, string, bool) {
	cidrs := cluster.Spec().Get("ceph", "networks", "clusterCIDRs").Strings()
	if len(cidrs) == 0 {
		return "", "", false
	}
	listed := strings.Join(cidrs, ", ")
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		m, found := c.Find(api.Machine, node.Get("machineRef").Text())
		if !found || insideAnyNetwork(machine.IPAddresses(m), cidrs) {
			continue
		}
		return "Ceph host " + storage.NativeHostname(node) + " (Machine " + m.Name() + ") has no address inside the replication networks " + listed,
			"add an address inside one of " + listed + " to the network of " + m.Name() + ", or remove spec.ceph.networks.clusterCIDRs", true
	}
	return "", "", false
}

func insideAnyNetwork(addresses, cidrs []string) bool {
	for _, value := range addresses {
		address, err := netip.ParseAddr(value)
		if err != nil {
			continue
		}
		for _, cidr := range cidrs {
			if prefix, err := netip.ParsePrefix(cidr); err == nil && prefix.Masked().Contains(address) {
				return true
			}
		}
	}
	return false
}

// selectDevices narrows destructive custody to the disks selected by native
// OSD documents. Other declared data disks never enter the Ceph envelope.
func selectDevices(r *Request) {
	selected := map[string]map[string]bool{}
	for _, raw := range r.Services {
		svc, err := reconciliation.DecodeEvidence[nativeService](raw, 60*1024, "Ceph native service")
		if err != nil || svc.ServiceType != "osd" || svc.Spec == nil {
			continue
		}
		for _, host := range svc.Placement.Hosts {
			if selected[host] == nil {
				selected[host] = map[string]bool{}
			}
			for _, path := range svc.Spec.DataDevices.Paths {
				selected[host][path] = true
			}
		}
	}
	for i := range r.Hosts {
		devices := []Device{}
		for _, d := range r.Hosts[i].Devices {
			if selected[r.Hosts[i].Name][d.Path] || selected[r.Hosts[i].Name]["/dev/disk/by-id/virtio-"+d.Serial] {
				devices = append(devices, d)
			}
		}
		r.Hosts[i].Devices = devices
	}
}

func checkDevices(r Request, names []string, cluster api.Object, service api.Value) error {
	for _, field := range service.Get("spec").Fields() {
		if field.Name != "data_devices" {
			return failure("lifecycle.unsupported", "an OSD allocation option is not modeled by the core adapter", "use only explicit data paths; the core profile uses native bluestore defaults")
		}
	}
	selector := service.Get("spec", "data_devices")
	if !selector.Has("paths") || selector.Len() != 1 || service.Get("spec").Has("db_devices") || service.Get("spec").Has("wal_devices") || service.Get("spec", "encrypted").Bool() || service.Get("spec", "osds_per_device").Present() && service.Get("spec", "osds_per_device").Text() != "1" {
		return failure("lifecycle.unsupported", "core OSDs require explicit owned virtual data paths without additional allocation options", "use data_devices.paths and one unencrypted OSD per dedicated data disk")
	}
	count, hasCount := service.Get("placement", "count").Int64()
	if hasCount && count != int64(len(names)) {
		return failure("lifecycle.unsupported", "core OSD allocation requires every explicitly selected host", "omit OSD placement.count or match the full selected host set")
	}
	for _, n := range names {
		native := ""
		for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
			if node.Get("name").Text() == n {
				native = storage.NativeHostname(node)
			}
		}
		for _, path := range selector.Get("paths").Items() {
			if path.Type() != api.String {
				return failure("lifecycle.unsupported", "core OSD paths must be plain device strings", "use plain owned data-disk paths: /dev/sd* for SSD-media disks, /dev/vd* otherwise")
			}
			p := path.Text()
			found := false
			for _, h := range r.Hosts {
				if h.Name == native {
					for _, d := range h.Devices {
						if d.Path == p {
							found = true
						}
					}
				}
			}
			if !found {
				return failure("api.invariant", "an OSD path is not an owned libvirt data disk", "select a declared dataDisks path; root disks cannot become OSDs")
			}
		}
	}
	return nil
}

func hostRequirement(host Host) reconciliation.ObjectRef {
	return reconciliation.ObjectRef{Kind: string(api.Machine), Object: host.Machine}
}
