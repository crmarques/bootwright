package v1alpha1

import (
	"slices"
	"testing"
)

func TestStorageWireShapesAreClosedAndVariantsAreExplicit(t *testing.T) {
	for _, kind := range []Kind{CephCluster, CephPlacementPolicy, CephPool, CephRBDImage, CephClient, CephFilesystem, CephSubvolume, CephObjectGateway, CephNFSExport, CephSMBResource, CephConsumer} {
		schema := Schema(kind)
		if schema == nil || schema.Type != Mapping || schema.Open {
			t.Fatalf("%s is not a registered closed object", kind)
		}
		if kind != CephCluster {
			if field, ok := schema.Field("clusterRef"); !ok || !field.Required || len(field.Shape.Reference) != 1 || field.Shape.Reference[0] != CephCluster {
				t.Fatalf("%s does not require its owning storage cluster", kind)
			}
		}
		checkStorageShape(t, schema)
	}
	fs, _ := Schema(CephFilesystem).Field("dataPoolRefs")
	if len(fs.Shape.Element.Alternatives) != 2 {
		t.Fatal("filesystem data pools must accept both scalar and named forms")
	}
	pool := Schema(CephPool)
	if replicated, _ := pool.Field("replicated"); !slices.Equal(pool.Arms, []string{"replicated", "erasure"}) || !replicated.Default.Present() {
		t.Fatal("pool protection must be the replicated or erasure presence arm, replicated by default")
	}
	erasure, _ := pool.Field("erasure")
	if erasure.Shape.Open {
		t.Fatal("erasure options must stay closed")
	}
	gateway := Schema(CephObjectGateway)
	for _, bound := range []string{"serviceRef", "realm", "zoneGroup", "zone"} {
		if f, ok := gateway.Field(bound); !ok || !f.Required {
			t.Fatalf("object gateway must require %s", bound)
		}
	}
	for _, retired := range []string{"serviceID", "placement", "frontendPort", "config"} {
		if _, ok := gateway.Field(retired); ok {
			t.Fatalf("object gateway still admits retired field %s", retired)
		}
	}
	endpoint, _ := gateway.Field("endpoint")
	if len(endpoint.Shape.Fields) != 1 || endpoint.Shape.Fields[0].Name != "fqdn" {
		t.Fatal("object gateway endpoint must carry only its FQDN")
	}
}

func TestCephAuxiliaryImagesAreClosedWithoutTheCephImageKey(t *testing.T) {
	ceph := Field{Shape: Schema(CephCluster)}
	images, found := ceph.Shape.Field("images")
	if !found || images.Shape.Open || images.Shape.Element != nil || len(images.Shape.Fields) != 15 {
		t.Fatal("ceph.images must be a closed record of the auxiliary image names")
	}
	if _, admitted := images.Shape.Field("base"); admitted {
		t.Fatal("ceph.images must not admit base; ceph.image owns the Ceph image")
	}
}

func TestNFSExportSchemaShape(t *testing.T) {
	schema := Schema(CephNFSExport)
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	if !slices.Equal(names, []string{"clusterRef", "serviceID", "filesystemRef", "subvolumeRef", "objectUserRef", "native"}) {
		t.Fatalf("CephNFSExport fields = %v", names)
	}
	for name, required := range map[string]bool{"serviceID": true, "native": true, "filesystemRef": false, "subvolumeRef": false, "objectUserRef": false} {
		if field, _ := schema.Field(name); field.Required != required {
			t.Fatalf("CephNFSExport %s required = %t", name, field.Required)
		}
	}
	if native, _ := schema.Field("native"); !native.Shape.Open || !native.Shape.Atomic {
		t.Fatal("native must be an open atomic mapping checked by admission")
	}
	for name, kind := range map[string]Kind{"filesystemRef": CephFilesystem, "subvolumeRef": CephSubvolume, "objectUserRef": CephObjectUser} {
		if field, _ := schema.Field(name); !slices.Equal(field.Shape.Reference, []Kind{kind}) {
			t.Fatalf("%s must reference %s", name, kind)
		}
	}
	record := StorageNFSExportNativeSchema()
	if record.Open {
		t.Fatal("the native export record must be closed")
	}
	checkStorageShape(t, record)
	fsal, _ := record.Field("fsal")
	for at, refused := range map[*Shape][]string{record: {"export_id", "cluster_id"}, fsal.Shape: {"user_id", "cephx_key", "sec_label_xattr", "access_key_id", "secret_access_key"}} {
		for _, name := range refused {
			if _, ok := at.Field(name); ok {
				t.Fatalf("the native export record admits refused member %s", name)
			}
		}
	}
	protocols, _ := record.Field("protocols")
	squash, _ := record.Field("squash")
	if protocols.Shape.Element.Minimum != "3" || protocols.Shape.Element.Maximum != "4" || len(squash.Shape.Enums) != 14 || !slices.Contains(squash.Shape.Enums, "all_anomnymous") {
		t.Fatal("protocols and squash must follow the native validation lists")
	}
}

func TestSMBResourceSchemaShape(t *testing.T) {
	schema := Schema(CephSMBResource)
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	if !slices.Equal(names, []string{"clusterRef", "smbClusterRef", "filesystemRef", "subvolumeRef", "resourceRefs", "secrets", "native"}) {
		t.Fatalf("CephSMBResource fields = %v", names)
	}
	if native, _ := schema.Field("native"); !native.Required || !native.Shape.Open || !native.Shape.Atomic {
		t.Fatal("native must be a required open atomic mapping checked by admission")
	}
	secrets, _ := schema.Field("secrets")
	ref, _ := secrets.Shape.Element.Field("secretRef")
	if secrets.Shape.NameKey != "path" || secrets.Shape.MinLength != 1 || !slices.Equal(ref.Shape.SecretTypes, []string{"opaque", "token"}) {
		t.Fatal("SMB password bindings take value Secrets only")
	}
	refused := map[string][]string{
		SMBCluster:     {"custom_smb_global_options", "client_compat", "custom_ports", "bind_addrs", "remote_control", "linked_to_cluster"},
		SMBShare:       {"comment", "max_connections", "custom_smb_share_options", "linked_to_cluster"},
		SMBJoinAuth:    {"linked_to_cluster"},
		SMBUsersGroups: {"linked_to_cluster"},
	}
	for resourceType, members := range refused {
		record := StorageSMBResourceNativeSchema(resourceType)
		if record == nil || record.Open {
			t.Fatalf("%s has no closed native record", resourceType)
		}
		checkStorageShape(t, record)
		for _, member := range members {
			if _, ok := record.Field(member); ok {
				t.Fatalf("%s admits refused member %s", resourceType, member)
			}
		}
	}
	auth, _ := StorageSMBResourceNativeSchema(SMBJoinAuth).Field("auth")
	values, _ := StorageSMBResourceNativeSchema(SMBUsersGroups).Field("values")
	users, _ := values.Shape.Field("users")
	addresses, _ := StorageSMBResourceNativeSchema(SMBCluster).Field("public_addrs")
	for _, at := range []*Shape{auth.Shape, users.Shape.Element, addresses.Shape.Element} {
		for _, member := range []string{"password", "destination"} {
			if _, ok := at.Field(member); ok {
				t.Fatalf("a native SMB record admits %s", member)
			}
		}
	}
	if StorageSMBResourceNativeSchema("ceph.smb.tls.credential") != nil {
		t.Fatal("TLS credential resources must have no native record")
	}
}

func TestNativeCoreSchemaAndBindingsUseOneClosedAuthoringPath(t *testing.T) {
	ceph := Field{Shape: Schema(CephCluster)}
	for _, retired := range []string{"monitoring", "mgmtGateway"} {
		if _, ok := ceph.Shape.Field(retired); ok {
			t.Fatalf("retired daemon helper %s still admitted", retired)
		}
	}
	services, ok := ceph.Shape.Field("services")
	if !ok || services.Shape.MinLength != 1 || services.Shape.Element.Open {
		t.Fatal("native service declarations must be non-empty and closed")
	}
	for _, native := range []string{"service_type", "service_id", "placement", "networks", "config", "spec", "extra_container_args", "custom_configs"} {
		if _, ok := services.Shape.Element.Field(native); !ok {
			t.Fatalf("missing native envelope field %s", native)
		}
	}
	for _, retired := range []string{"serviceType", "serviceID"} {
		if _, ok := services.Shape.Element.Field(retired); ok {
			t.Fatalf("retired native alias %s still admitted", retired)
		}
	}
	placement, _ := services.Shape.Element.Field("placement")
	for _, native := range []string{"hosts", "label", "host_pattern", "count", "count_per_host"} {
		if _, ok := placement.Shape.Field(native); !ok {
			t.Fatalf("missing native placement %s", native)
		}
	}
	topology, _ := ceph.Shape.Field("topology")
	nodes, _ := topology.Shape.Field("nodes")
	for _, retired := range []string{"roles", "devices", "osd"} {
		if _, ok := nodes.Shape.Element.Field(retired); ok {
			t.Fatalf("retired topology helper %s still admitted", retired)
		}
	}
	bindings, _ := ceph.Shape.Field("serviceBindings")
	secrets, _ := bindings.Shape.Element.Field("secrets")
	ref, _ := secrets.Shape.Element.Field("secretRef")
	if bindings.Shape.NameKey != "serviceRef" || secrets.Shape.NameKey != "path" || len(ref.Shape.Reference) != 1 || ref.Shape.Reference[0] != Secret {
		t.Fatal("native binding must retain a typed descriptor-only reference")
	}
	for _, serviceType := range []string{"mon", "mgr", "osd", "mds", "rgw", "ingress", "nfs"} {
		shape := StorageNativeServiceSpecSchema(serviceType)
		if shape == nil || shape.Open {
			t.Fatalf("native core %s lacks a closed payload", serviceType)
		}
		checkStorageShape(t, shape)
	}
	if mds := StorageNativeServiceSpecSchema("mds"); len(mds.Fields) != 0 {
		t.Fatal("native MDS spec carries no type-specific field")
	}
	for serviceType, refused := range map[string][]string{
		"rgw":     {"rgw_frontend_ssl_certificate", "ssl_cert", "ssl_key"},
		"ingress": {"monitor_password", "keepalived_password", "ssl_cert"},
		"nfs":     {"ssl", "kmip_key", "enable_nfsv3"},
	} {
		for _, name := range refused {
			if _, ok := StorageNativeServiceSpecSchema(serviceType).Field(name); ok {
				t.Fatalf("native %s record admits refused member %s", serviceType, name)
			}
		}
	}
	if StorageNativeServiceSpecSchema("container") != nil {
		t.Fatal("future family schema must be qualified by its owning slice")
	}
	client := Schema(CephClient)
	for name, kind := range map[string]Kind{"poolRef": CephPool, "filesystemRef": CephFilesystem, "subvolumeRef": CephSubvolume} {
		if scope, ok := client.Field(name); !ok || scope.Required || scope.Shape.Reference[0] != kind {
			t.Fatalf("client scope %s must be an optional reference to %s", name, kind)
		}
	}
	cluster, _ := client.Field("clusterRef")
	access, _ := client.Field("access")
	if !cluster.Required || !access.Required || len(access.Shape.Enums) != 2 {
		t.Fatal("client must bind its cluster and explicit bounded access")
	}
}
func TestRBDImageSchemaIsClosedWithExactBounds(t *testing.T) {
	schema := Schema(CephRBDImage)
	if schema == nil || schema.Open {
		t.Fatal("CephRBDImage must be a closed object")
	}
	for _, name := range []string{"clusterRef", "poolRef", "sizeBytes"} {
		if field, ok := schema.Field(name); !ok || !field.Required {
			t.Fatalf("CephRBDImage must require %s", name)
		}
	}
	size, _ := schema.Field("sizeBytes")
	if size.Shape.Type != Integer || size.Shape.Minimum != "1" || size.Shape.Maximum != "9223372036854775807" {
		t.Fatalf("sizeBytes bounds = %q..%q", size.Shape.Minimum, size.Shape.Maximum)
	}
	features, ok := schema.Field("features")
	if !ok || features.Required || !features.Shape.Unique || features.Shape.Element == nil || !slices.Equal(features.Shape.Element.Enums, []string{"layering", "exclusive-lock", "object-map", "fast-diff", "deep-flatten", "journaling"}) {
		t.Fatal("features must be an optional unique set of the six admitted names in librbd bit order")
	}
	dataPool, ok := schema.Field("dataPoolRef")
	if !ok || dataPool.Required || !slices.Equal(dataPool.Shape.Reference, []Kind{CephPool}) {
		t.Fatal("dataPoolRef must be an optional CephPool reference")
	}
	if len(schema.Fields) != 5 {
		t.Fatalf("CephRBDImage admits %d fields, want 5", len(schema.Fields))
	}
}

func checkStorageShape(t *testing.T, shape *Shape) {
	t.Helper()
	seen := map[string]bool{}
	for _, field := range shape.Fields {
		if field.Name == "" || seen[field.Name] {
			t.Fatal("ambiguous storage wire schema")
		}
		seen[field.Name] = true
		checkStorageShape(t, field.Shape)
	}
	if shape.Element != nil {
		checkStorageShape(t, shape.Element)
	}
	for _, alternative := range shape.Alternatives {
		checkStorageShape(t, alternative)
	}
}

func TestFilesystemSchemaBindsItsMDSServiceAndRetiresDaemonHelpers(t *testing.T) {
	schema := Schema(CephFilesystem)
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	if !slices.Equal(names, []string{"clusterRef", "mdsServiceRef", "metadataPoolRef", "dataPoolRefs", "mds", "subvolumeGroups"}) {
		t.Fatalf("CephFilesystem fields = %v", names)
	}
	if ref, _ := schema.Field("mdsServiceRef"); ref.Required || ref.Shape.Type != String {
		t.Fatal("mdsServiceRef must be an optional string, derived as mds.<name>")
	}
	mds, _ := schema.Field("mds")
	bounds := map[string][2]string{}
	for _, field := range mds.Shape.Fields {
		bounds[field.Name] = [2]string{field.Shape.Minimum, field.Shape.Maximum}
	}
	if len(bounds) != 3 || bounds["activeCount"] != [2]string{"1", "256"} || bounds["standbyCountWanted"] != [2]string{"0", "2147483647"} {
		t.Fatalf("mds fields = %v", bounds)
	}
	for _, retired := range []string{"placement", "serviceSpec"} {
		if _, ok := mds.Shape.Field(retired); ok {
			t.Fatalf("mds.%s must be retired", retired)
		}
	}
	groups, _ := schema.Field("subvolumeGroups")
	size, _ := groups.Shape.Element.Field("sizeBytes")
	uid, _ := groups.Shape.Element.Field("uid")
	if size.Shape.Minimum != "1" || size.Shape.Maximum != "9223372036854775807" || uid.Shape.Minimum != "0" || uid.Shape.Maximum != "2147483647" {
		t.Fatal("group sizeBytes must be positive and uid an int32")
	}
}

func TestSubvolumeSchemaIsClosedWithExactBounds(t *testing.T) {
	schema := Schema(CephSubvolume)
	if schema == nil || schema.Open {
		t.Fatal("CephSubvolume must be a closed object")
	}
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
	}
	if !slices.Equal(names, []string{"clusterRef", "filesystemRef", "group", "sizeBytes", "poolLayoutRef", "uid", "gid", "mode"}) {
		t.Fatalf("CephSubvolume fields = %v", names)
	}
	for name, required := range map[string]bool{"clusterRef": true, "filesystemRef": true, "group": false, "sizeBytes": false, "poolLayoutRef": false, "uid": false, "gid": false, "mode": false} {
		if field, _ := schema.Field(name); field.Required != required {
			t.Fatalf("CephSubvolume %s required = %t", name, field.Required)
		}
	}
	for name, bounds := range map[string][2]string{"sizeBytes": {"1", "9223372036854775807"}, "uid": {"0", "2147483647"}, "gid": {"0", "2147483647"}} {
		if field, _ := schema.Field(name); field.Shape.Type != Integer || field.Shape.Minimum != bounds[0] || field.Shape.Maximum != bounds[1] {
			t.Fatalf("%s bounds = %q..%q", name, field.Shape.Minimum, field.Shape.Maximum)
		}
	}
	for name, kind := range map[string]Kind{"clusterRef": CephCluster, "filesystemRef": CephFilesystem, "poolLayoutRef": CephPool} {
		if field, _ := schema.Field(name); !slices.Equal(field.Shape.Reference, []Kind{kind}) {
			t.Fatalf("%s must reference %s", name, kind)
		}
	}
	for _, name := range []string{"group", "mode"} {
		if field, _ := schema.Field(name); field.Shape.Type != String {
			t.Fatalf("%s must be a string", name)
		}
	}
}

func TestB479ServiceSchemasAreClosed(t *testing.T) {
	refused := map[string][]string{
		"prometheus":    {"targets", "remote_write_url", "remote_write_allowed_metrics", "ssl", "certificate_source"},
		"alertmanager":  {"ssl", "certificate_source", "secure"},
		"grafana":       {"ssl", "certificate_source", "initial_admin_password"},
		"node-exporter": {"targets", "remote_write_url", "remote_write_allowed_metrics", "ssl", "certificate_source"},
		"ceph-exporter": {"ssl", "certificate_source", "sock_dir", "addrs"},
		"snmp-gateway":  {"credentials"},
		"mgmt-gateway":  {"ssl_cert", "ssl_key", "custom_sans", "certificate_source", "ssl_prefer_server_ciphers", "ssl_session_tickets", "ssl_session_timeout", "ssl_session_cache", "server_tokens", "ssl_stapling", "ssl_stapling_verify"},
		"oauth2-proxy":  {"ssl", "certificate_source", "ssl_cert", "ssl_key", "custom_sans", "client_secret", "cookie_secret", "ssl_insecure_skip_verify"},
		"nvmeof":        {"server_key", "server_cert", "client_key", "client_cert", "root_ca_cert", "encryption_key", "addr", "addr_map", "discovery_addr", "discovery_addr_map", "enable_prometheus_exporter", "prometheus_port"},
		"loki":          {"retention_time", "targets", "only_bind_port_on_networks"},
		"alloy":         {"retention_time", "targets", "only_bind_port_on_networks"},
		"promtail":      {"retention_time", "targets", "only_bind_port_on_networks"},
	}
	required := map[string][]string{"nvmeof": {"pool", "group"}, "mgmt-gateway": {"port"}, "snmp-gateway": {"snmp_version", "snmp_destination"}, "oauth2-proxy": {"provider_display_name", "client_id", "oidc_issuer_url"}}
	var closed func(*Shape) bool
	closed = func(shape *Shape) bool {
		if shape.Type == Mapping && shape.Open {
			return false
		}
		for _, field := range shape.Fields {
			if !closed(field.Shape) {
				return false
			}
		}
		return shape.Element == nil || closed(shape.Element)
	}
	for serviceType, members := range refused {
		shape := StorageNativeServiceSpecSchema(serviceType)
		if shape == nil || !closed(shape) {
			t.Fatalf("%s lacks a closed native record", serviceType)
		}
		checkStorageShape(t, shape)
		for _, member := range members {
			if _, ok := shape.Field(member); ok {
				t.Fatalf("%s admits refused member %s", serviceType, member)
			}
		}
		for _, field := range shape.Fields {
			if field.Required != slices.Contains(required[serviceType], field.Name) {
				t.Fatalf("%s.%s required = %v", serviceType, field.Name, field.Required)
			}
		}
	}
	ceph := Field{Shape: Schema(CephCluster)}
	cephadm, _ := ceph.Shape.Field("cephadm")
	if _, ok := cephadm.Shape.Field("workarounds"); ok {
		t.Fatal("the retired cephadm workaround list is still admitted")
	}
}

func TestNVMeoFSubsystemSchemaShape(t *testing.T) {
	schema := Schema(CephNVMeoFSubsystem)
	names := []string{}
	for _, field := range schema.Fields {
		names = append(names, field.Name)
		if !field.Required {
			t.Fatalf("CephNVMeoFSubsystem %s must be required", field.Name)
		}
	}
	if !slices.Equal(names, []string{"clusterRef", "serviceRef", "nqn", "listeners", "hosts", "namespaces"}) || schema.Open {
		t.Fatalf("CephNVMeoFSubsystem fields = %v", names)
	}
	checkStorageShape(t, schema)
	listeners, _ := schema.Field("listeners")
	namespaces, _ := schema.Field("namespaces")
	hosts, _ := schema.Field("hosts")
	if listeners.Shape.NameKey != "host" || listeners.Shape.MinLength != 1 || namespaces.Shape.NameKey != "nsid" || namespaces.Shape.MinLength != 1 || !hosts.Shape.Unique || hosts.Shape.MinLength != 1 {
		t.Fatal("listeners, namespaces and hosts must be nonempty sets keyed by host, nsid and value")
	}
	nsid, _ := namespaces.Shape.Element.Field("nsid")
	image, _ := namespaces.Shape.Element.Field("imageRef")
	port, _ := listeners.Shape.Element.Field("trsvcid")
	if nsid.Shape.Minimum != "1" || nsid.Shape.Maximum != "4294967294" || !slices.Equal(image.Shape.Reference, []Kind{CephRBDImage}) || port.Required || port.Shape.Maximum != "65535" {
		t.Fatal("a namespace ID is an NVMe namespace ID backed by a CephRBDImage, and trsvcid is an optional port")
	}
	for _, refused := range []string{"maxNamespaces", "serialNumber", "enableHA", "dhchapKey", "psk", "loadBalancingGroup"} {
		if _, ok := schema.Field(refused); ok {
			t.Fatalf("CephNVMeoFSubsystem admits %s", refused)
		}
	}
}
