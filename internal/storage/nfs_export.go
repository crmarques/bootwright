package storage

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type nfsExportRefusal struct {
	path            []string
	message, remedy string
}

const nfsExportKeyless = "an export declaration carries no key; the NFS manager supplies its credentials"

var nfsExportRefusedMembers = []nfsExportRefusal{
	{path: []string{"export_id"}, message: "the NFS manager generates the export ID", remedy: "omit spec.native.export_id; Bootwright records the generated ID"},
	{path: []string{"cluster_id"}, message: "the NFS cluster is the service spec.serviceID names", remedy: "omit spec.native.cluster_id"},
	{path: []string{"fsal", "user_id"}, message: "the binding supplies the export's principal: the NFS manager derives a CephFS one, and spec.objectUserRef names the RGW one", remedy: "omit spec.native.fsal.user_id"},
	{path: []string{"fsal", "cephx_key"}, message: nfsExportKeyless, remedy: "omit spec.native.fsal.cephx_key"},
	{path: []string{"fsal", "access_key_id"}, message: nfsExportKeyless, remedy: "omit spec.native.fsal.access_key_id"},
	{path: []string{"fsal", "secret_access_key"}, message: nfsExportKeyless, remedy: "omit spec.native.fsal.secret_access_key"},
	{path: []string{"fsal", "sec_label_xattr"}, message: "this NFS export option has no qualified effect contract", remedy: "omit spec.native.fsal.sec_label_xattr"},
}

var nfsExportBackings = []string{"filesystemRef", "subvolumeRef", "objectUserRef"}

const (
	nfsExportBacking    = "an NFS export requires exactly one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef"
	nfsExportV3         = "NFSv3 is not supported by Bootwright yet"
	nfsExportV4Remedy   = "set spec.native.protocols: [4]; NFS exports serve NFSv4"
	nfsExportPathRemedy = "use one clean absolute path"
)

func (a *admission) withoutRefusedExportMembers(native api.Value) api.Value {
	for _, row := range nfsExportRefusedMembers {
		parent := native.Get(row.path[:len(row.path)-1]...)
		leaf := row.path[len(row.path)-1]
		if parent.Type() != api.Mapping || !parent.Has(leaf) {
			continue
		}
		a.issue("api.invariant", "$.spec.native."+strings.Join(row.path, "."), row.message, row.remedy)
		native = native.WithPath(parent.Without(leaf), row.path[:len(row.path)-1]...)
	}
	return native
}

func (a *admission) nfsExportIntrinsic() {
	spec := a.object.Spec()
	if nfsExportBackingCount(spec) > 1 {
		a.issue("api.invariant", "$.spec", nfsExportBacking, "keep only one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef")
	}
	native := spec.Get("native")
	if native.Type() != api.Mapping {
		return
	}
	native = a.withoutRefusedExportMembers(native)
	a.nativeShape(native, api.StorageNFSExportNativeSchema(), "$.spec.native", a.partial)
	a.nfsExportPaths(native)
	if slices.ContainsFunc(native.Get("protocols").Items(), func(item api.Value) bool { return item.Type() == api.Integer && item.Text() == "3" }) {
		a.issue("api.invariant", "$.spec.native.protocols", nfsExportV3, nfsExportV4Remedy)
	}
	if contains(native.Get("transports"), "UDP") {
		a.issue("api.invariant", "$.spec.native.transports", "UDP is an incompatible transport for NFSv4 exports", "set spec.native.transports: [TCP]")
	}
	if slices.ContainsFunc(native.Get("sectype").Strings(), func(token string) bool { return strings.HasPrefix(token, "krb5") }) {
		a.issue("api.invariant", "$.spec.native.sectype", "Kerberos is not supported by Bootwright yet", "set spec.native.sectype: [sys] or omit spec.native.sectype")
	}
	a.nfsExportFSAL(native)
	a.nfsExportClients(native.Get("clients"))
}

func nfsExportBackingCount(spec api.Value) int {
	count := 0
	for _, key := range nfsExportBackings {
		if spec.Has(key) {
			count++
		}
	}
	return count
}

func (a *admission) nfsExportFSAL(native api.Value) {
	name := native.Get("fsal", "name")
	if name.Type() != api.String {
		return
	}
	if name.Text() != "RGW" {
		return
	}
	for _, member := range []string{"fs_name", "cmount_path"} {
		if native.Get("fsal").Has(member) {
			a.issue("api.invariant", "$.spec.native.fsal."+member, member+" is a CephFS member that the RGW FSAL does not hold", "omit spec.native.fsal."+member)
		}
	}
	if path := native.Get("path"); path.Type() == api.String && path.Text() != "/" {
		a.issue("api.invariant", "$.spec.native.path", "an export of an RGW user serves the path /, whose entries are the user's buckets", "set spec.native.path: / or omit it")
	}
}

func (a *admission) nfsExportPaths(native api.Value) {
	if pseudo := native.Get("pseudo"); pseudo.Type() == api.String && (!cleanAbsolute(pseudo.Text()) || pseudo.Text() == "/") {
		a.issue("api.value", "$.spec.native.pseudo", "an NFS pseudo path must be clean, absolute and not /", "use one clean absolute path below /")
	}
	clean := map[string]bool{}
	for _, at := range [][]string{{"path"}, {"fsal", "cmount_path"}} {
		value := native.Get(at...)
		if value.Type() != api.String {
			continue
		}
		if clean[at[len(at)-1]] = cleanAbsolute(value.Text()); !clean[at[len(at)-1]] {
			a.issue("api.value", "$.spec.native."+strings.Join(at, "."), "NFS path must be clean and absolute", nfsExportPathRemedy)
		}
	}
	path, cmount := textOf(native, "path"), textOf(native, "fsal", "cmount_path")
	if clean["path"] && clean["cmount_path"] && cmount != "/" && cmount != path && !strings.HasPrefix(path, cmount+"/") {
		a.issue("api.invariant", "$.spec.native.fsal.cmount_path", "cmount_path must be / or an ancestor directory of the export path", "set spec.native.fsal.cmount_path to / or to a directory that contains spec.native.path, or omit it")
	}
}

func canonicalClient(text string) string {
	if prefix, err := netip.ParsePrefix(text); err == nil {
		return prefix.Masked().String()
	}
	if ip, err := netip.ParseAddr(text); err == nil {
		return ip.String()
	}
	return ""
}

func (a *admission) nfsExportClients(clients api.Value) {
	seen := map[string]bool{}
	for i, client := range clients.Items() {
		field := indexed("$.spec.native.clients", i)
		addresses := client.Get("addresses")
		if addresses.Type() != api.Sequence {
			continue
		}
		if addresses.Len() == 0 {
			a.issue("api.invariant", field+".addresses", "an NFS client entry needs at least one address", "list one or more literal IP addresses or CIDRs under addresses")
		}
		for j, address := range addresses.Items() {
			if address.Type() != api.String {
				continue
			}
			canonical := canonicalClient(address.Text())
			switch {
			case canonical == "":
				a.issue("api.value", indexed(field+".addresses", j), "NFS client must be an IP or CIDR", "use a literal IP address or CIDR")
			case seen[canonical]:
				a.invalid(field+".addresses", "NFS clients must be unique after canonicalization")
			}
			seen[canonical] = canonical != ""
		}
	}
}

func (a *admission) nfsExport(cluster api.Object) {
	spec := a.object.Spec()
	a.required(spec, "native", "$.spec")
	if nfsExportBackingCount(spec) == 0 {
		a.issue("api.required", "$.spec", nfsExportBacking, "set spec.filesystemRef or spec.subvolumeRef for CephFS, or spec.objectUserRef for an RGW user's buckets")
	}
	if spec.Has("objectUserRef") {
		a.nfsExportObjectUser(cluster, spec)
	}
	switch name := textOf(spec, "native", "fsal", "name"); {
	case name == "RGW" && !spec.Has("objectUserRef"):
		a.issue("api.invariant", "$.spec.native.fsal.name", "an RGW export serves the buckets of the StorageObjectUser that spec.objectUserRef names", "set spec.objectUserRef to a StorageObjectUser of this cluster, or set spec.native.fsal.name: CEPH and bind spec.filesystemRef or spec.subvolumeRef")
	case name == "CEPH" && spec.Has("objectUserRef"):
		a.issue("api.invariant", "$.spec.native.fsal.name", "spec.objectUserRef exports an RGW user's buckets, which only the RGW FSAL serves", "set spec.native.fsal.name: RGW, or replace spec.objectUserRef with spec.filesystemRef or spec.subvolumeRef")
	}
	filesystem := textOf(spec, "filesystemRef")
	if spec.Has("filesystemRef") {
		a.sameOwner(api.StorageFilesystem, filesystem, "$.spec.filesystemRef", cluster)
	}
	if spec.Has("subvolumeRef") {
		if subvolume, ok := a.sameOwner(api.StorageSubvolume, textOf(spec, "subvolumeRef"), "$.spec.subvolumeRef", cluster); ok && !spec.Has("filesystemRef") {
			filesystem = textOf(subvolume.Spec(), "filesystemRef")
		}
	}
	if _, ok := NativeService(cluster, "nfs."+textOf(spec, "serviceID")); spec.Has("serviceID") && !ok {
		a.issue("api.invariant", "$.spec.serviceID", "spec.serviceID resolves to no declared nfs service of StorageCluster "+cluster.Name(), "add a spec.ceph.services entry with service_type nfs and this service_id to StorageCluster "+cluster.Name()+", or set spec.serviceID to the service_id of a declared nfs service")
	}
	native := spec.Get("native")
	if native.Type() != api.Mapping {
		return
	}
	if !native.Has("protocols") {
		a.issue("api.invariant", "$.spec.native.protocols", "an export without protocols serves NFSv3 and NFSv4, and "+nfsExportV3, nfsExportV4Remedy)
	}
	if name := native.Get("fsal", "fs_name"); name.Present() && filesystem != "" && name.Text() != filesystem {
		a.issue("api.invariant", "$.spec.native.fsal.fs_name", "fs_name disagrees with the filesystem the export's binding selects", "omit spec.native.fsal.fs_name so the binding supplies it, or set it to the bound filesystem's name")
	}
	a.nfsExportPseudo(cluster, spec)
}

func (a *admission) nfsExportObjectUser(cluster api.Object, spec api.Value) {
	if _, ok := a.sameOwner(api.StorageObjectUser, textOf(spec, "objectUserRef"), "$.spec.objectUserRef", cluster); !ok {
		return
	}
	gateways := []string{}
	for _, gateway := range a.catalog.OfKind(api.StorageObjectGateway) {
		if textOf(gateway.Spec(), "clusterRef") == cluster.Name() {
			gateways = append(gateways, gateway.Name())
		}
	}
	if len(gateways) > 1 {
		slices.Sort(gateways)
		a.issue("api.invariant", "$.spec.objectUserRef", "StorageNFSExport "+a.object.Name()+" exports an RGW user, which the NFS manager and NFS-Ganesha reach only through the cluster's default realm, and only one StorageObjectGateway can own it; StorageCluster "+cluster.Name()+" declares the gateways "+strings.Join(gateways, ", "),
			"keep one StorageObjectGateway in StorageCluster "+cluster.Name()+", or remove spec.objectUserRef and this export")
	}
}

func (a *admission) nfsExportPseudo(cluster api.Object, spec api.Value) {
	pseudo := spec.Get("native", "pseudo")
	if pseudo.Type() != api.String {
		return
	}
	for _, other := range a.catalog.OfKind(api.StorageNFSExport) {
		peer := other.Spec()
		if other.Name() == a.object.Name() || textOf(peer, "clusterRef") != cluster.Name() || textOf(peer, "serviceID") != textOf(spec, "serviceID") {
			continue
		}
		if peer.Get("native", "pseudo").Equal(pseudo) {
			a.issue("api.invariant", "$.spec.native.pseudo", "this pseudo path is already exported by StorageNFSExport "+other.Name()+" on the same NFS service", "choose a pseudo path no other export of this NFS service uses")
			return
		}
	}
}
