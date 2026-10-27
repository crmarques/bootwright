package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func nfsExportCatalog(t *testing.T, services ...api.Value) api.Catalog {
	t.Helper()
	_, catalog := withGraph(t, fixtureCatalog(), append([]api.Value{nfsService(m())}, services...))
	return normalizeCatalog(replace(catalog, object(api.CephSubvolume, "docs", m("clusterRef", "storage", "filesystemRef", "files"))))
}

func nfsExportWith(t *testing.T, catalog api.Catalog, change func(api.Value) api.Value) (api.Object, api.Catalog) {
	t.Helper()
	export := find(t, catalog, api.CephNFSExport, "nfs")
	export = export.WithSpec(change(export.Spec()))
	return export, replace(catalog, export)
}

func nativeEdit(value api.Value, path ...string) func(api.Value) api.Value {
	return func(spec api.Value) api.Value { return spec.WithPath(value, append([]string{"native"}, path...)...) }
}

func requireAdmits(t *testing.T, object api.Object, catalog api.Catalog) {
	t.Helper()
	if issues := Validate(object, catalog); len(issues) != 0 {
		t.Fatalf("admissible %s refused: %#v", object.Identity(), issues)
	}
}

func exactIssue(t *testing.T, issues []api.Issue, code, field, message, remedy string) {
	t.Helper()
	found := issuesAt(issues, field)
	if len(found) != 1 || found[0].Code != code || found[0].Message != message || (remedy != "" && found[0].Remediation != remedy) {
		t.Fatalf("issues at %s: %#v (all %#v)", field, found, issues)
	}
}

func TestNFSExportNativeRecordIsClosed(t *testing.T) {
	catalog := nfsExportCatalog(t)
	requireAdmits(t, find(t, catalog, api.CephNFSExport, "nfs"), catalog)
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.IntegerValue("30"), "attr_expiration_time"))
	exactIssue(t, Validate(export, changed), "api.field", "$.spec.native", "native field is not in the supported schema", "remove the unsupported field at $.spec.native")
	keyless := "an export declaration carries no key; the NFS manager supplies its credentials"
	for _, row := range []struct {
		path            []string
		message, remedy string
	}{
		{[]string{"export_id"}, "the NFS manager generates the export ID", "omit spec.native.export_id; Bootwright records the generated ID"},
		{[]string{"cluster_id"}, "the NFS cluster is the service spec.serviceID names", "omit spec.native.cluster_id"},
		{[]string{"fsal", "user_id"}, "the binding supplies the export's principal: the NFS manager derives a CephFS one, and spec.objectUserRef names the RGW one", "omit spec.native.fsal.user_id"},
		{[]string{"fsal", "cephx_key"}, keyless, "omit spec.native.fsal.cephx_key"},
		{[]string{"fsal", "sec_label_xattr"}, "this NFS export option has no qualified effect contract", "omit spec.native.fsal.sec_label_xattr"},
		{[]string{"fsal", "access_key_id"}, keyless, "omit spec.native.fsal.access_key_id"},
		{[]string{"fsal", "secret_access_key"}, keyless, "omit spec.native.fsal.secret_access_key"},
	} {
		export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringValue("s3cr3t-value"), row.path...))
		issues := Validate(export, changed)
		if len(issues) != 1 {
			t.Fatalf("refused member %v produced other issues: %#v", row.path, issues)
		}
		exactIssue(t, issues, "api.invariant", "$.spec.native."+strings.Join(row.path, "."), row.message, row.remedy)
		for _, issue := range issues {
			if strings.Contains(issue.Message+issue.Remediation, "s3cr3t-value") {
				t.Fatalf("a refused member's value escaped into %#v", issue)
			}
		}
		if partial := ValidatePartial(export, changed); len(partial) != 1 || len(issuesAt(partial, "$.spec.native."+strings.Join(row.path, "."))) != 1 {
			t.Fatalf("partial validation of refused member %v: %#v", row.path, partial)
		}
	}
}

func TestNFSExportProtocolsDefaultRefusesV3(t *testing.T) {
	catalog := nfsExportCatalog(t)
	omitted, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value { return spec.With("native", spec.Get("native").Without("protocols")) })
	normalized, _ := Normalize(omitted, changed)
	if normalized.Spec().Has("native", "protocols") {
		t.Fatal("normalization froze protocols")
	}
	issues := Validate(normalized, replace(changed, normalized))
	if len(issues) != 1 {
		t.Fatalf("omitted protocols: %#v", issues)
	}
	exactIssue(t, issues, "api.invariant", "$.spec.native.protocols", "an export without protocols serves NFSv3 and NFSv4, and NFSv3 is not supported by Bootwright yet", "set spec.native.protocols: [4]; NFS exports serve NFSv4")
	if partial := ValidatePartial(omitted, changed); len(partial) != 0 {
		t.Fatalf("a partial fragment without protocols was refused: %#v", partial)
	}
	for _, protocols := range [][]string{{"3"}, {"3", "4"}} {
		items := []api.Value{}
		for _, p := range protocols {
			items = append(items, api.IntegerValue(p))
		}
		export, changed := nfsExportWith(t, catalog, nativeEdit(api.ListValue(items...), "protocols"))
		exactIssue(t, Validate(export, changed), "api.invariant", "$.spec.native.protocols", "NFSv3 is not supported by Bootwright yet", "set spec.native.protocols: [4]; NFS exports serve NFSv4")
	}
	requireAdmits(t, find(t, catalog, api.CephNFSExport, "nfs"), catalog)
}

func TestNFSExportTransportsTCPOnly(t *testing.T) {
	catalog := nfsExportCatalog(t)
	for _, transports := range [][]string{{"UDP"}, {"TCP", "UDP"}} {
		export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringList(transports...), "transports"))
		exactIssue(t, Validate(export, changed), "api.invariant", "$.spec.native.transports", "UDP is an incompatible transport for NFSv4 exports", "set spec.native.transports: [TCP]")
	}
	if got := find(t, catalog, api.CephNFSExport, "nfs").Spec().Get("native", "transports"); !got.Equal(api.StringList("TCP")) {
		t.Fatalf("omitted transports normalized to %v", got.Strings())
	}
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringList("TCP"), "transports"))
	requireAdmits(t, export, changed)
}

func TestNFSExportPseudoUniquePerService(t *testing.T) {
	more := nfsService(m()).With("service_id", api.StringValue("more"))
	catalog := nfsExportCatalog(t, more)
	nfs := find(t, catalog, api.CephNFSExport, "nfs")
	twin := object(api.CephNFSExport, "twin", nfs.Spec())
	catalog = replace(catalog, twin)
	exactIssue(t, Validate(nfs, catalog), "api.invariant", "$.spec.native.pseudo", "this pseudo path is already exported by CephNFSExport twin on the same NFS service", "choose a pseudo path no other export of this NFS service uses")
	exactIssue(t, Validate(twin, catalog), "api.invariant", "$.spec.native.pseudo", "this pseudo path is already exported by CephNFSExport nfs on the same NFS service", "")
	twin = twin.WithSpec(twin.Spec().With("serviceID", api.StringValue("more")))
	catalog = replace(catalog, twin)
	requireAdmits(t, nfs, catalog)
	requireAdmits(t, twin, catalog)
	catalog = replace(catalog, object(api.CephCluster, "other", find(t, catalog, api.CephCluster, "storage").Spec()))
	catalog = replace(catalog, object(api.CephFilesystem, "other-files", m("clusterRef", "other", "mdsServiceRef", "mds.other-files", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("data"))))
	far := object(api.CephNFSExport, "far", nfs.Spec().With("clusterRef", api.StringValue("other")).With("filesystemRef", api.StringValue("other-files")).WithPath(api.StringValue("other-files"), "native", "fsal", "fs_name"))
	catalog = replace(catalog, far)
	if textOf(far.Spec(), "serviceID") != textOf(nfs.Spec(), "serviceID") || !far.Spec().Get("native", "pseudo").Equal(nfs.Spec().Get("native", "pseudo")) {
		t.Fatal("the far export must share the service ID and pseudo path of nfs")
	}
	requireAdmits(t, nfs, catalog)
	requireAdmits(t, far, catalog)
}

func TestNFSExportBackingExactlyOne(t *testing.T) {
	catalog := nfsExportCatalog(t)
	none, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value { return spec.Without("filesystemRef") })
	backing := "an NFS export requires exactly one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef"
	exactIssue(t, Validate(none, changed), "api.required", "$.spec", backing, "set spec.filesystemRef or spec.subvolumeRef for CephFS, or spec.objectUserRef for an RGW user's buckets")
	both, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value { return spec.With("subvolumeRef", api.StringValue("docs")) })
	exactIssue(t, Validate(both, changed), "api.invariant", "$.spec", backing, "keep only one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef")
	exactIssue(t, ValidatePartial(both, changed), "api.invariant", "$.spec", backing, "keep only one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef")
	subvolume, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.Without("filesystemRef").With("subvolumeRef", api.StringValue("docs"))
	})
	subvolume, _ = Normalize(subvolume, changed)
	requireAdmits(t, subvolume, replace(changed, subvolume))
	catalog = replace(catalog, object(api.CephFilesystem, "foreign", m("clusterRef", "elsewhere", "mdsServiceRef", "mds.foreign", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("data"))))
	foreign, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.With("filesystemRef", api.StringValue("foreign")).With("native", spec.Get("native").With("fsal", m("name", "CEPH")))
	})
	exactIssue(t, Validate(foreign, changed), "api.invariant", "$.spec.filesystemRef", "referenced storage child belongs to a different cluster", "")
	catalog = replace(catalog, object(api.CephSubvolume, "foreign-docs", m("clusterRef", "elsewhere", "filesystemRef", "foreign")))
	foreignSubvolume, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.Without("filesystemRef").With("subvolumeRef", api.StringValue("foreign-docs")).With("native", spec.Get("native").With("fsal", m("name", "CEPH")))
	})
	exactIssue(t, Validate(foreignSubvolume, changed), "api.invariant", "$.spec.subvolumeRef", "referenced storage child belongs to a different cluster", "")
}

func TestNFSExportCmountPrefixOfPath(t *testing.T) {
	catalog := nfsExportCatalog(t)
	if textOf(find(t, catalog, api.CephNFSExport, "nfs").Spec(), "native", "fsal", "cmount_path") != "/" {
		t.Fatal("omitted cmount_path was not normalized to /")
	}
	at := func(path, cmount string) []api.Issue {
		export, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
			return spec.WithPath(api.StringValue(path), "native", "path").WithPath(api.StringValue(cmount), "native", "fsal", "cmount_path")
		})
		return Validate(export, changed)
	}
	for _, pair := range [][2]string{{"/a/b", "/"}, {"/a", "/a"}, {"/a/b", "/a"}} {
		if issues := at(pair[0], pair[1]); len(issues) != 0 {
			t.Fatalf("path %s with cmount %s refused: %#v", pair[0], pair[1], issues)
		}
	}
	exactIssue(t, at("/ab", "/a"), "api.invariant", "$.spec.native.fsal.cmount_path", "cmount_path must be / or an ancestor directory of the export path", "set spec.native.fsal.cmount_path to / or to a directory that contains spec.native.path, or omit it")
	exactIssue(t, at("/a/b", "/a/"), "api.value", "$.spec.native.fsal.cmount_path", "NFS path must be clean and absolute", "use one clean absolute path")
	exactIssue(t, at("a/b", "/"), "api.value", "$.spec.native.path", "NFS path must be clean and absolute", "use one clean absolute path")
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringValue("/"), "pseudo"))
	exactIssue(t, Validate(export, changed), "api.value", "$.spec.native.pseudo", "an NFS pseudo path must be clean, absolute and not /", "use one clean absolute path below /")
}

func TestNFSExportClientEntryRequiresAllThree(t *testing.T) {
	catalog := nfsExportCatalog(t)
	client := m("addresses", api.StringList("192.0.2.123/24"), "access_type", "RW", "squash", "root_squash")
	withClients := func(entries ...api.Value) []api.Issue {
		export, changed := nfsExportWith(t, catalog, nativeEdit(api.ListValue(entries...), "clients"))
		export, _ = Normalize(export, changed)
		return Validate(export, replace(changed, export))
	}
	for _, key := range []string{"addresses", "access_type", "squash"} {
		requireIssue(t, withClients(client.Without(key)), "$.spec.native.clients[0]."+key)
	}
	exactIssue(t, withClients(client.With("addresses", api.ListValue())), "api.invariant", "$.spec.native.clients[0].addresses", "an NFS client entry needs at least one address", "list one or more literal IP addresses or CIDRs under addresses")
	exactIssue(t, withClients(client.With("addresses", api.StringList("host.example.test"))), "api.value", "$.spec.native.clients[0].addresses[0]", "NFS client must be an IP or CIDR", "use a literal IP address or CIDR")
	exactIssue(t, withClients(client, client.With("addresses", api.StringList("192.0.2.0/24"))), "api.invariant", "$.spec.native.clients[1].addresses", "NFS clients must be unique after canonicalization", "")
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.ListValue(client), "clients"))
	normalized, _ := Normalize(export, changed)
	if got := normalized.Spec().Get("native", "clients").Items()[0].Get("addresses"); !got.Equal(api.StringList("192.0.2.0/24")) {
		t.Fatalf("client addresses normalized to %v", got.Strings())
	}
	requireAdmits(t, normalized, replace(changed, normalized))
}

func TestNFSExportRetiredFields(t *testing.T) {
	for field, replacement := range map[string]string{
		"$.spec.port":      "spec.port of the nfs service that spec.serviceID names",
		"$.spec.placement": "the placement of the nfs service that spec.serviceID names",
		"$.spec.ingresses": "an ingress service in its CephCluster's spec.services whose spec.backend_service is the nfs service that spec.serviceID names",
		"$.spec.exports":   "one CephNFSExport per export entry, each binding spec.filesystemRef or spec.subvolumeRef and holding that entry's native export under spec.native",
	} {
		issue, ok := RetiredField(api.CephNFSExport, field)
		if !ok || issue.Code != "api.field" || issue.Field != field || issue.Remediation != "replace "+field+" with "+replacement {
			t.Fatalf("retired %s: %#v", field, issue)
		}
	}
	for _, field := range []string{"$.spec.serviceID", "$.spec.native", "$.spec.filesystemRef", "$.spec.subvolumeRef"} {
		if _, ok := RetiredField(api.CephNFSExport, field); ok {
			t.Fatalf("current field %s is retired", field)
		}
	}
}

func TestNFSExportServiceIDResolvesDeclaredNFSService(t *testing.T) {
	_, catalog := withGraph(t, fixtureCatalog(), []api.Value{rgwService(m()).With("service_id", api.StringValue("files"))})
	export := find(t, catalog, api.CephNFSExport, "nfs")
	exactIssue(t, Validate(export, catalog), "api.invariant", "$.spec.serviceID", "spec.serviceID resolves to no declared nfs service of CephCluster storage", "add a spec.services entry with service_type nfs and this service_id to CephCluster storage, or set spec.serviceID to the service_id of a declared nfs service")
	catalog = nfsExportCatalog(t)
	requireAdmits(t, find(t, catalog, api.CephNFSExport, "nfs"), catalog)
}

func TestNFSExportFrozenDefaultsAndBindingFSName(t *testing.T) {
	catalog := nfsExportCatalog(t)
	export := find(t, catalog, api.CephNFSExport, "nfs")
	native := export.Spec().Get("native")
	for _, want := range []struct {
		path  []string
		value api.Value
	}{
		{[]string{"access_type"}, api.StringValue("RO")},
		{[]string{"squash"}, api.StringValue("no_root_squash")},
		{[]string{"security_label"}, api.BoolValue(true)},
		{[]string{"transports"}, api.StringList("TCP")},
		{[]string{"path"}, api.StringValue("/")},
		{[]string{"fsal", "cmount_path"}, api.StringValue("/")},
		{[]string{"fsal", "fs_name"}, api.StringValue("files")},
	} {
		if !native.Get(want.path...).Equal(want.value) {
			t.Fatalf("frozen default %v = %#v", want.path, native.Get(want.path...))
		}
	}
	if native.Has("sectype") {
		t.Fatal("normalization froze sectype")
	}
	if again, _ := Normalize(export, catalog); !again.Spec().Equal(export.Spec()) {
		t.Fatal("export normalization is not idempotent")
	}
	authored, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.With("native", m("pseudo", "/files", "access_type", "RW", "squash", "all_squash", "protocols", []api.Value{v(4)}, "fsal", m("name", "CEPH")))
	})
	authored, _ = Normalize(authored, changed)
	if textOf(authored.Spec(), "native", "access_type") != "RW" || textOf(authored.Spec(), "native", "squash") != "all_squash" {
		t.Fatal("normalization replaced authored values")
	}
	subvolume, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.Without("filesystemRef").With("subvolumeRef", api.StringValue("docs")).With("native", m("pseudo", "/docs", "protocols", []api.Value{v(4)}, "fsal", m("name", "CEPH")))
	})
	subvolume, _ = Normalize(subvolume, changed)
	if textOf(subvolume.Spec(), "native", "fsal", "fs_name") != "files" || subvolume.Spec().Has("native", "path") {
		t.Fatalf("subvolume binding normalized to %#v", subvolume.Spec().Get("native"))
	}
	requireAdmits(t, subvolume, replace(changed, subvolume))
	disagreement := func(export api.Object, catalog api.Catalog) {
		t.Helper()
		exactIssue(t, Validate(export, catalog), "api.invariant", "$.spec.native.fsal.fs_name", "fs_name disagrees with the filesystem the export's binding selects", "omit spec.native.fsal.fs_name so the binding supplies it, or set it to the bound filesystem's name")
	}
	other, changed := nfsExportWith(t, catalog, nativeEdit(api.StringValue("other"), "fsal", "fs_name"))
	disagreement(other, changed)
	otherSubvolume, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.Without("filesystemRef").With("subvolumeRef", api.StringValue("docs")).WithPath(api.StringValue("other"), "native", "fsal", "fs_name")
	})
	otherSubvolume, _ = Normalize(otherSubvolume, changed)
	if textOf(otherSubvolume.Spec(), "native", "fsal", "fs_name") != "other" {
		t.Fatal("normalization replaced the authored fs_name of a subvolume binding")
	}
	disagreement(otherSubvolume, replace(changed, otherSubvolume))
}

func TestNFSExportRefusesKerberos(t *testing.T) {
	catalog := nfsExportCatalog(t)
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringList("krb5p"), "sectype"))
	exactIssue(t, Validate(export, changed), "api.invariant", "$.spec.native.sectype", "Kerberos is not supported by Bootwright yet", "set spec.native.sectype: [sys] or omit spec.native.sectype")
	export, changed = nfsExportWith(t, catalog, nativeEdit(api.StringList("sys"), "sectype"))
	requireAdmits(t, export, changed)
	export, changed = nfsExportWith(t, catalog, nativeEdit(api.StringList("none"), "sectype"))
	requireIssue(t, Validate(export, changed), "$.spec.native.sectype[0]")
}

func objectExportCatalog(t *testing.T) (api.Object, api.Catalog) {
	t.Helper()
	_, catalog := withMDSService(t, fixtureCatalog(), rgwService(qualifiedRGWSpec()))
	_, catalog = withGraph(t, catalog, []api.Value{nfsService(m())})
	catalog = replace(catalog, objectUser("app-writer"))
	export := object(api.CephNFSExport, "buckets", m("clusterRef", "storage", "serviceID", "files", "objectUserRef", "app-writer", "native",
		m("pseudo", "/buckets", "protocols", []api.Value{v(4)}, "fsal", m("name", "RGW"))))
	export, _ = Normalize(export, replace(catalog, export))
	return export, replace(catalog, export)
}

func TestNFSExportOfAnObjectUserServesItsBuckets(t *testing.T) {
	export, catalog := objectExportCatalog(t)
	requireAdmits(t, export, catalog)
	if !export.Spec().Get("native", "path").Equal(api.StringValue("/")) || export.Spec().Has("native", "fsal", "cmount_path") || export.Spec().Has("native", "fsal", "fs_name") {
		t.Fatalf("an RGW export normalized to %#v", export.Spec().Get("native"))
	}
	if again, _ := Normalize(export, catalog); !again.Spec().Equal(export.Spec()) {
		t.Fatal("RGW export normalization is not idempotent")
	}
	for _, member := range []string{"fs_name", "cmount_path"} {
		changed := export.WithSpec(export.Spec().WithPath(api.StringValue("/"), "native", "fsal", member))
		exactIssue(t, Validate(changed, replace(catalog, changed)), "api.invariant", "$.spec.native.fsal."+member, member+" is a CephFS member that the RGW FSAL does not hold", "omit spec.native.fsal."+member)
		exactIssue(t, ValidatePartial(changed, replace(catalog, changed)), "api.invariant", "$.spec.native.fsal."+member, member+" is a CephFS member that the RGW FSAL does not hold", "")
	}
	bucket := export.WithSpec(export.Spec().WithPath(api.StringValue("/app-data"), "native", "path"))
	exactIssue(t, Validate(bucket, replace(catalog, bucket)), "api.invariant", "$.spec.native.path", "an export of an RGW user serves the path /, whose entries are the user's buckets", "set spec.native.path: / or omit it")
	ceph := export.WithSpec(export.Spec().WithPath(m("name", "CEPH"), "native", "fsal"))
	exactIssue(t, Validate(ceph, replace(catalog, ceph)), "api.invariant", "$.spec.native.fsal.name", "spec.objectUserRef exports an RGW user's buckets, which only the RGW FSAL serves", "set spec.native.fsal.name: RGW, or replace spec.objectUserRef with spec.filesystemRef or spec.subvolumeRef")
	if partial := ValidatePartial(ceph, replace(catalog, ceph)); len(partial) != 0 {
		t.Fatalf("a partial fragment was refused for a cross-field FSAL choice: %#v", partial)
	}
	unbound := export.WithSpec(export.Spec().Without("objectUserRef").With("filesystemRef", api.StringValue("files")))
	exactIssue(t, Validate(unbound, replace(catalog, unbound)), "api.invariant", "$.spec.native.fsal.name", "an RGW export serves the buckets of the CephObjectUser that spec.objectUserRef names", "set spec.objectUserRef to a CephObjectUser of this cluster, or set spec.native.fsal.name: CEPH and bind spec.filesystemRef or spec.subvolumeRef")
	both := export.WithSpec(export.Spec().With("filesystemRef", api.StringValue("files")))
	exactIssue(t, ValidatePartial(both, replace(catalog, both)), "api.invariant", "$.spec", "an NFS export requires exactly one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef", "keep only one of spec.filesystemRef, spec.subvolumeRef and spec.objectUserRef")
	catalog = replace(catalog, objectUser("far-writer", "clusterRef", "elsewhere"))
	user := export.WithSpec(export.Spec().With("objectUserRef", api.StringValue("far-writer")))
	exactIssue(t, Validate(user, replace(catalog, user)), "api.invariant", "$.spec.objectUserRef", "referenced storage child belongs to a different cluster", "")
}

func TestNFSExportOfAnObjectUserRefusesASecondGateway(t *testing.T) {
	export, catalog := objectExportCatalog(t)
	_, catalog = withGraph(t, catalog, []api.Value{moreRGW()})
	catalog = replace(catalog, secondGateway())
	exactIssue(t, Validate(export, catalog), "api.invariant", "$.spec.objectUserRef",
		"CephNFSExport buckets exports an RGW user, which the NFS manager and NFS-Ganesha reach only through the cluster's default realm, and only one CephObjectGateway can own it; CephCluster storage declares the gateways objects, second",
		"keep one CephObjectGateway in CephCluster storage, or remove spec.objectUserRef and this export")
	foreign := object(api.CephCluster, "other", find(t, catalog, api.CephCluster, "storage").Spec())
	catalog = replace(replace(catalog, foreign), secondGateway("clusterRef", "other"))
	requireAdmits(t, export, catalog)
}
