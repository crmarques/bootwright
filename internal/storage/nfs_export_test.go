package storage

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func nfsExportCatalog(t *testing.T, services ...api.Value) api.Catalog {
	t.Helper()
	_, catalog := withGraph(t, fixtureCatalog(), append([]api.Value{nfsService(m())}, services...))
	return normalizeCatalog(replace(catalog, object(api.StorageSubvolume, "docs", m("clusterRef", "storage", "filesystemRef", "files"))))
}

func nfsExportWith(t *testing.T, catalog api.Catalog, change func(api.Value) api.Value) (api.Object, api.Catalog) {
	t.Helper()
	export := find(t, catalog, api.StorageNFSExport, "nfs")
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
	requireAdmits(t, find(t, catalog, api.StorageNFSExport, "nfs"), catalog)
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.IntegerValue("30"), "attr_expiration_time"))
	exactIssue(t, Validate(export, changed), "api.field", "$.spec.native", "native field is not in the supported schema", "remove the unsupported field at $.spec.native")
	keyless := "an export declaration carries no key; the NFS manager supplies its credentials"
	for _, row := range []struct {
		path            []string
		message, remedy string
	}{
		{[]string{"export_id"}, "the NFS manager generates the export ID", "omit spec.native.export_id; Bootwright records the generated ID"},
		{[]string{"cluster_id"}, "the NFS cluster is the service spec.serviceID names", "omit spec.native.cluster_id"},
		{[]string{"fsal", "user_id"}, "the NFS manager derives the CephFS principal of an export", "omit spec.native.fsal.user_id"},
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
	requireAdmits(t, find(t, catalog, api.StorageNFSExport, "nfs"), catalog)
}

func TestNFSExportTransportsTCPOnly(t *testing.T) {
	catalog := nfsExportCatalog(t)
	for _, transports := range [][]string{{"UDP"}, {"TCP", "UDP"}} {
		export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringList(transports...), "transports"))
		exactIssue(t, Validate(export, changed), "api.invariant", "$.spec.native.transports", "UDP is an incompatible transport for NFSv4 exports", "set spec.native.transports: [TCP]")
	}
	if got := find(t, catalog, api.StorageNFSExport, "nfs").Spec().Get("native", "transports"); !got.Equal(api.StringList("TCP")) {
		t.Fatalf("omitted transports normalized to %v", got.Strings())
	}
	export, changed := nfsExportWith(t, catalog, nativeEdit(api.StringList("TCP"), "transports"))
	requireAdmits(t, export, changed)
}

func TestNFSExportPseudoUniquePerService(t *testing.T) {
	more := nfsService(m()).With("service_id", api.StringValue("more"))
	catalog := nfsExportCatalog(t, more)
	nfs := find(t, catalog, api.StorageNFSExport, "nfs")
	twin := object(api.StorageNFSExport, "twin", nfs.Spec())
	catalog = replace(catalog, twin)
	exactIssue(t, Validate(nfs, catalog), "api.invariant", "$.spec.native.pseudo", "this pseudo path is already exported by StorageNFSExport twin on the same NFS service", "choose a pseudo path no other export of this NFS service uses")
	exactIssue(t, Validate(twin, catalog), "api.invariant", "$.spec.native.pseudo", "this pseudo path is already exported by StorageNFSExport nfs on the same NFS service", "")
	twin = twin.WithSpec(twin.Spec().With("serviceID", api.StringValue("more")))
	catalog = replace(catalog, twin)
	requireAdmits(t, nfs, catalog)
	requireAdmits(t, twin, catalog)
}

func TestNFSExportBackingExactlyOne(t *testing.T) {
	catalog := nfsExportCatalog(t)
	none, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value { return spec.Without("filesystemRef") })
	exactIssue(t, Validate(none, changed), "api.required", "$.spec", "an NFS export requires exactly one of spec.filesystemRef and spec.subvolumeRef", "set spec.filesystemRef or spec.subvolumeRef")
	both, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value { return spec.With("subvolumeRef", api.StringValue("docs")) })
	exactIssue(t, Validate(both, changed), "api.invariant", "$.spec", "an NFS export requires exactly one of spec.filesystemRef and spec.subvolumeRef", "keep only one of spec.filesystemRef and spec.subvolumeRef")
	exactIssue(t, ValidatePartial(both, changed), "api.invariant", "$.spec", "an NFS export requires exactly one of spec.filesystemRef and spec.subvolumeRef", "keep only one of spec.filesystemRef and spec.subvolumeRef")
	subvolume, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.Without("filesystemRef").With("subvolumeRef", api.StringValue("docs"))
	})
	subvolume, _ = Normalize(subvolume, changed)
	requireAdmits(t, subvolume, replace(changed, subvolume))
	catalog = replace(catalog, object(api.StorageFilesystem, "foreign", m("clusterRef", "elsewhere", "mdsServiceRef", "mds.foreign", "metadataPoolRef", "metadata", "dataPoolRefs", api.StringList("data"))))
	foreign, changed := nfsExportWith(t, catalog, func(spec api.Value) api.Value {
		return spec.With("filesystemRef", api.StringValue("foreign")).With("native", spec.Get("native").With("fsal", m("name", "CEPH")))
	})
	exactIssue(t, Validate(foreign, changed), "api.invariant", "$.spec.filesystemRef", "referenced storage child belongs to a different cluster", "")
}

func TestNFSExportCmountPrefixOfPath(t *testing.T) {
	catalog := nfsExportCatalog(t)
	if textOf(find(t, catalog, api.StorageNFSExport, "nfs").Spec(), "native", "fsal", "cmount_path") != "/" {
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
		"$.spec.ingresses": "an ingress service in its StorageCluster's spec.ceph.services whose spec.backend_service is the nfs service that spec.serviceID names",
		"$.spec.exports":   "one StorageNFSExport per export entry, each binding spec.filesystemRef or spec.subvolumeRef and holding that entry's native export under spec.native",
	} {
		issue, ok := RetiredField(api.StorageNFSExport, field)
		if !ok || issue.Code != "api.field" || issue.Field != field || issue.Remediation != "replace "+field+" with "+replacement {
			t.Fatalf("retired %s: %#v", field, issue)
		}
	}
	for _, field := range []string{"$.spec.serviceID", "$.spec.native", "$.spec.filesystemRef", "$.spec.subvolumeRef"} {
		if _, ok := RetiredField(api.StorageNFSExport, field); ok {
			t.Fatalf("current field %s is retired", field)
		}
	}
}

func TestNFSExportServiceIDResolvesDeclaredNFSService(t *testing.T) {
	_, catalog := withGraph(t, fixtureCatalog(), []api.Value{rgwService(m()).With("service_id", api.StringValue("files"))})
	export := find(t, catalog, api.StorageNFSExport, "nfs")
	exactIssue(t, Validate(export, catalog), "api.invariant", "$.spec.serviceID", "spec.serviceID resolves to no declared nfs service of StorageCluster storage", "add a spec.ceph.services entry with service_type nfs and this service_id to StorageCluster storage, or set spec.serviceID to the service_id of a declared nfs service")
	catalog = nfsExportCatalog(t)
	requireAdmits(t, find(t, catalog, api.StorageNFSExport, "nfs"), catalog)
}

func TestNFSExportFrozenDefaultsAndBindingFSName(t *testing.T) {
	catalog := nfsExportCatalog(t)
	export := find(t, catalog, api.StorageNFSExport, "nfs")
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
	other, changed := nfsExportWith(t, catalog, nativeEdit(api.StringValue("other"), "fsal", "fs_name"))
	exactIssue(t, Validate(other, changed), "api.invariant", "$.spec.native.fsal.fs_name", "fs_name disagrees with the filesystem the export's binding selects", "omit spec.native.fsal.fs_name so the binding supplies it, or set it to the bound filesystem's name")
}

func TestNFSExportRefusesObjectBackingAndKerberos(t *testing.T) {
	catalog := nfsExportCatalog(t)
	export, changed := nfsExportWith(t, catalog, nativeEdit(m("name", "RGW"), "fsal"))
	exactIssue(t, Validate(export, changed), "api.invariant", "$.spec.native.fsal.name", "NFS exports of object storage are not supported by Bootwright yet", "bind spec.filesystemRef or spec.subvolumeRef and set spec.native.fsal.name: CEPH")
	export, changed = nfsExportWith(t, catalog, nativeEdit(api.StringList("krb5p"), "sectype"))
	exactIssue(t, Validate(export, changed), "api.invariant", "$.spec.native.sectype", "Kerberos is not supported by Bootwright yet", "set spec.native.sectype: [sys] or omit spec.native.sectype")
	export, changed = nfsExportWith(t, catalog, nativeEdit(api.StringList("sys"), "sectype"))
	requireAdmits(t, export, changed)
	export, changed = nfsExportWith(t, catalog, nativeEdit(api.StringList("none"), "sectype"))
	requireIssue(t, Validate(export, changed), "$.spec.native.sectype[0]")
}
