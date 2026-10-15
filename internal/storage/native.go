package storage

import (
	"math/big"
	"net/netip"
	"path"
	"regexp"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var nativeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func NativeHostname(node api.Value) string {
	return fallback(node.Get("fqdn"), textOf(node, "name"))
}

func nativeNode(cluster api.Object, hostname string) (api.Value, bool) {
	var result api.Value
	count := 0
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		if hostname != "" && NativeHostname(node) == hostname {
			result = node
			count++
		}
	}
	return result, count == 1
}

func NativeServiceName(service api.Value) string {
	typeName, id := textOf(service, "service_type"), textOf(service, "service_id")
	if id == "" {
		return typeName
	}
	return typeName + "." + id
}

func ResolveServicePlacement(cluster api.Object, service api.Value) ([]string, []api.Issue) {
	a := admission{object: cluster}
	nodes := a.nativePlacement(cluster, service.Get("placement"), "$.placement", false)
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, textOf(node, "name"))
	}
	slices.Sort(names)
	return names, a.issues
}

func hostToken(value api.Value) (hostname, network, daemon string) {
	if value.Type() == api.Mapping {
		return textOf(value, "hostname"), textOf(value, "network"), textOf(value, "name")
	}
	hostname = value.Text()
	if before, after, ok := strings.Cut(hostname, "="); ok {
		hostname, daemon = before, after
	}
	if before, after, ok := strings.Cut(hostname, ":"); ok {
		hostname, network = before, after
	}
	return
}

func (a *admission) nativePlacement(cluster api.Object, placement api.Value, field string, partial bool) []api.Value {
	hosts := placement.Get("hosts").Items()
	label, pattern := placement.Get("label"), placement.Get("host_pattern")
	modes := 0
	for _, present := range []bool{placement.Has("hosts"), label.Present(), pattern.Present()} {
		if present {
			modes++
		}
	}
	if modes > 1 {
		a.invalid(field, "native hosts, label and host_pattern selectors are mutually exclusive")
	}
	if placement.Has("count") && placement.Has("count_per_host") {
		a.invalid(field, "native count and count_per_host are mutually exclusive")
	}
	if placement.Has("count_per_host") && len(hosts) == 0 && !label.Present() && !pattern.Present() {
		a.invalid(field+".count_per_host", "native count_per_host must be combined with label or hosts or host_pattern")
	}
	if placement.Has("count_per_host") && slices.ContainsFunc(hosts, func(host api.Value) bool {
		_, network, daemon := hostToken(host)
		return network != "" || daemon != ""
	}) {
		a.invalid(field+".count_per_host", "native count_per_host cannot be combined with explicit placement names or networks")
	}
	for _, key := range []string{"count", "count_per_host"} {
		if placement.Has(key) && !positive(placement.Get(key)) {
			a.invalid(field+"."+key, "native placement counts must be positive integers")
		}
	}
	var match func(string) bool
	if pattern.Present() {
		value, kind := pattern.Text(), "fnmatch"
		if pattern.Type() == api.Mapping {
			value, kind = textOf(pattern, "pattern"), fallback(pattern.Get("pattern_type"), "fnmatch")
		}
		if kind == "regex" {
			compiled, err := regexp.Compile("^(?:" + value + ")")
			if err != nil {
				a.invalid(field+".host_pattern", "native host pattern must be a valid supported regular expression")
			} else {
				match = compiled.MatchString
			}
		} else if kind == "fnmatch" {
			glob := nativeGlob(value)
			if _, err := path.Match(glob, ""); err != nil {
				a.invalid(field+".host_pattern", "native host pattern must be a valid hostname glob")
			} else {
				match = func(host string) bool { ok, _ := path.Match(glob, host); return ok }
			}
		} else {
			a.invalid(field+".host_pattern", "native host pattern type is unsupported")
		}
	}
	if partial {
		return nil
	}
	if !placement.Present() {
		a.required(api.MapValue(), "placement", strings.TrimSuffix(field, ".placement"))
	}
	wanted := map[string]bool{}
	for i, host := range hosts {
		name, network, daemon := hostToken(host)
		node, ok := nativeNode(cluster, name)
		if !ok {
			a.invalid(indexed(field+".hosts", i), "native hostname must resolve to a registered storage-node FQDN")
			continue
		}
		if wanted[name] {
			a.invalid(field+".hosts", "native placement host identities must be unique")
		}
		wanted[name] = true
		if daemon != "" && !nativeID.MatchString(daemon) {
			a.invalid(indexed(field+".hosts", i), "native daemon identifier has an unsupported form")
		}
		if network != "" {
			a.nativeHostNetwork(node, network, indexed(field+".hosts", i))
		}
	}
	selected := []api.Value{}
	for _, node := range cluster.Spec().Get("ceph", "topology", "nodes").Items() {
		name := NativeHostname(node)
		if placement.Has("hosts") && !wanted[name] {
			continue
		}
		if label.Present() && !contains(node.Get("labels"), label.Text()) {
			continue
		}
		if pattern.Present() && (match == nil || !match(name)) {
			continue
		}
		selected = append(selected, node)
	}
	if len(selected) == 0 {
		a.invalid(field, "native placement must select at least one declared storage host")
	}
	if count := placement.Get("count"); count.Present() && numeric(count).Cmp(big.NewInt(int64(len(selected)))) > 0 {
		a.invalid(field+".count", "native placement count exceeds the eligible declared host set")
	}
	return selected
}

func nativeGlob(value string) string {
	var result strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			result.WriteString(`\\`)
			continue
		}
		result.WriteByte(value[i])
		if value[i] == '[' && i+1 < len(value) {
			if value[i+1] == '!' {
				result.WriteByte('^')
				i++
			} else if value[i+1] == '^' {
				result.WriteString(`\^`)
				i++
			}
		}
	}
	return result.String()
}

func (a *admission) nativeHostNetwork(node api.Value, network, field string) {
	ip, ipErr := netip.ParseAddr(network)
	prefix, prefixErr := netip.ParsePrefix(network)
	if ipErr != nil && prefixErr != nil {
		a.invalid(field, "native host network must be a proved IP address or CIDR")
		return
	}
	if len(a.catalog.Objects()) == 0 {
		return
	}
	machine, ok := a.catalog.Find(api.Machine, textOf(node, "machineRef"))
	if !ok {
		return
	}
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		declared, err := netip.ParsePrefix(textOf(address, "address"))
		if err == nil && ((ipErr == nil && declared.Addr() == ip) || (prefixErr == nil && prefix.Masked() == declared.Masked())) {
			return
		}
	}
	a.invalid(field, "native host network must agree with the bound Machine address")
}

func (a *admission) nativeShape(value api.Value, shape *api.Shape, field string, partial bool) {
	if !value.Present() {
		if !partial {
			for _, f := range shape.Fields {
				if f.Required {
					a.required(value, f.Name, field)
				}
			}
		}
		return
	}
	if len(shape.Alternatives) > 0 {
		for _, alternative := range shape.Alternatives {
			if alternative.Type == value.Type() {
				a.nativeShape(value, alternative, field, partial)
				return
			}
		}
		a.issue("api.type", field, "native value has the wrong type", "use the qualified native field type at "+field)
		return
	}
	if value.Type() != shape.Type && !(shape.Type == api.Number && value.Type() == api.Integer) {
		a.issue("api.type", field, "native value has the wrong type", "use the qualified native field type at "+field)
		return
	}
	switch shape.Type {
	case api.Mapping:
		for _, f := range value.Fields() {
			if child, ok := shape.Field(f.Name); ok {
				a.nativeShape(f.Value, child.Shape, field+"."+f.Name, partial)
			} else if shape.Open && shape.Element != nil {
				a.nativeShape(f.Value, shape.Element, field, partial)
			} else {
				a.issue("api.field", field, "native field is not in the supported schema", "remove the unsupported field at "+field)
			}
		}
		if !partial {
			for _, f := range shape.Fields {
				if f.Required && !value.Has(f.Name) {
					a.required(value, f.Name, field)
				}
			}
		}
	case api.Sequence:
		for i, item := range value.Items() {
			a.nativeShape(item, shape.Element, indexed(field, i), partial)
			if shape.Unique {
				for _, earlier := range value.Items()[:i] {
					if earlier.Equal(item) {
						a.invalid(field, "native set members must be unique")
					}
				}
			}
		}
	case api.Integer, api.Number:
		for _, bound := range []struct {
			text    string
			minimum bool
		}{{shape.Minimum, true}, {shape.Maximum, false}} {
			if bound.text == "" {
				continue
			}
			n, ok := new(big.Rat).SetString(value.Text())
			limit, valid := new(big.Rat).SetString(bound.text)
			if !ok || !valid || (bound.minimum && n.Cmp(limit) < 0) || (!bound.minimum && n.Cmp(limit) > 0) {
				a.invalid(field, "native numeric value is outside its supported bounds")
			}
		}
	case api.String:
		if shape.MinLength > 0 && value.Text() == "" {
			a.invalid(field, "native string must not be empty")
		}
		if len(shape.Enums) != 0 && !slices.Contains(shape.Enums, value.Text()) {
			a.invalid(field, "native value is not a supported option")
		}
		if shape.Rule == "device-path" && !cleanDevice(value.Text()) {
			a.invalid(field, "native device path must be a clean absolute descendant of /dev")
		}
	}
}

type serviceIDRule int

const (
	serviceIDForbidden serviceIDRule = iota
	serviceIDNative
	serviceIDBootwright
)

type nativeServiceType struct {
	name, label    string
	serviceID      serviceIDRule
	noLeadingDigit bool
	core           bool
}

var nativeServiceTypes = []nativeServiceType{
	{name: "mon", label: "MON", serviceID: serviceIDForbidden, core: true},
	{name: "mgr", label: "MGR", serviceID: serviceIDForbidden, core: true},
	{name: "osd", label: "OSD", serviceID: serviceIDBootwright, core: true},
	{name: "mds", label: "MDS", serviceID: serviceIDNative, noLeadingDigit: true},
	{name: "rgw", label: "RGW", serviceID: serviceIDNative},
	{name: "ingress", label: "ingress", serviceID: serviceIDNative},
	{name: "nfs", label: "NFS", serviceID: serviceIDNative},
}

func nativeServiceTypeOf(name string) (nativeServiceType, bool) {
	for _, row := range nativeServiceTypes {
		if row.name == name {
			return row, true
		}
	}
	return nativeServiceType{}, false
}

func implementedServiceRemedy() string {
	labels := make([]string, 0, len(nativeServiceTypes))
	for _, row := range nativeServiceTypes {
		labels = append(labels, row.label)
	}
	last := len(labels) - 1
	return "select an implemented " + strings.Join(labels[:last], ", ") + " or " + labels[last] + " service, or wait for its owning service slice"
}

func NativeService(cluster api.Object, ref string) (api.Value, bool) {
	var result api.Value
	count := 0
	for _, service := range cluster.Spec().Get("ceph", "services").Items() {
		if NativeServiceName(service) == ref {
			result = service
			count++
		}
	}
	return result, count == 1
}

func (a *admission) nativeServiceID(service api.Value, row nativeServiceType, field string, partial bool) {
	id := textOf(service, "service_id")
	switch {
	case service.Has("service_id") && (row.serviceID == serviceIDForbidden || !nativeID.MatchString(id)):
		a.invalid(field+".service_id", "native singleton services omit service_id; other IDs use the qualified name grammar")
	case service.Has("service_id") && row.noLeadingDigit && id[0] >= '0' && id[0] <= '9':
		a.invalid(field+".service_id", row.label+" service IDs cannot start with a numeric digit")
	case !service.Has("service_id") && row.serviceID != serviceIDForbidden && !partial:
		a.required(service, "service_id", field)
	case service.Has("service_id") && row.name == "mds" && !partial:
		a.mdsBinding(id, field)
	}
}

func (a *admission) mdsBinding(id, field string) {
	if filesystem, ok := a.catalog.Find(api.StorageFilesystem, id); ok && textOf(filesystem.Spec(), "clusterRef") == a.object.Name() {
		return
	}
	a.issue("api.invariant", field+".service_id", "MDS service is not bound by a StorageFilesystem of this cluster", "declare a StorageFilesystem of this cluster whose name is this service_id, or remove the MDS service")
}

func (a *admission) nativeServices(ceph api.Value, partial bool) {
	services := ceph.Get("services").Items()
	seen := map[string]bool{}
	core := map[string]bool{}
	for i, service := range services {
		field := indexed("$.spec.ceph.services", i)
		for _, pair := range [][2]string{{"serviceType", "service_type"}, {"serviceID", "service_id"}} {
			if service.Has(pair[0]) {
				a.retired(field + "." + pair[0])
			}
		}
		typeName := textOf(service, "service_type")
		shape := api.StorageNativeServiceSpecSchema(typeName)
		if typeName == "" && partial {
			continue
		}
		row, known := nativeServiceTypeOf(typeName)
		if !known || shape == nil {
			a.issue("api.invariant", field+".service_type", "native service family is not implemented by this storage consumer", implementedServiceRemedy())
			continue
		}
		name := NativeServiceName(service)
		if seen[name] {
			a.invalid(field+".service_id", "qualified native service identities must be unique")
		}
		seen[name], core[typeName] = true, true
		a.nativeServiceID(service, row, field, partial)
		if service.Has("service_name") && textOf(service, "service_name") != name {
			a.invalid(field+".service_name", "native service_name must agree with its qualified identity")
		}
		for _, key := range []string{"unmanaged", "preview_only"} {
			if service.Get(key).Bool() {
				a.invalid(field+"."+key, "owned services must remain managed and executable")
			}
		}
		for _, key := range []string{"extra_container_args", "extra_entrypoint_args", "custom_configs"} {
			if service.Get(key).Len() != 0 {
				a.issue("api.invariant", field+"."+key, "native effect-bearing options are not qualified by this core consumer", "omit "+field+"."+key+" until its effect contract is qualified")
			}
		}
		a.nativeShape(a.withoutRefusedMembers(service.Get("spec"), typeName, field+".spec"), shape, field+".spec", partial)
		placed := a.nativePlacement(a.object, service.Get("placement"), field+".placement", partial)
		a.nativeFamily(service, typeName, field, placed, partial)
		if typeName == "osd" {
			a.nativeOSD(service.Get("spec"), field+".spec", placed, partial)
		}
		if typeName == "mon" {
			for _, location := range service.Get("spec", "crush_locations").Fields() {
				node, ok := nativeNode(a.object, location.Name)
				if !partial && (!ok || !slices.ContainsFunc(placed, func(selected api.Value) bool { return textOf(selected, "name") == textOf(node, "name") })) {
					a.invalid(field+".spec.crush_locations", "monitor CRUSH locations must resolve to eligible registered hosts")
				}
				for _, entry := range location.Value.Strings() {
					key, value, valid := strings.Cut(entry, "=")
					if !valid || !nativeID.MatchString(key) || !nativeID.MatchString(value) || key == "host" {
						a.invalid(field+".spec.crush_locations", "monitor CRUSH location entries must use qualified bucket=value identities")
					} else if !partial && node.Has("location", key) && textOf(node, "location", key) != value {
						a.invalid(field+".spec.crush_locations", "monitor CRUSH location must agree with declared host topology")
					}
				}
			}
		}
		if !partial {
			a.serviceNetworks(a.object, service.Get("networks"), field+".networks")
		}
		for _, config := range service.Get("config").Fields() {
			if slices.Contains([]string{"public_network", "cluster_network", "container_image"}, config.Name) {
				a.invalid(field+".config", "native config cannot repeat a typed or frozen field")
			}
		}
		if service.Get("config").Len() != 0 {
			a.invalid(field+".config", "native service config options need a release-qualified effect and confidential-field record")
		}
	}
	if !partial {
		for _, row := range nativeServiceTypes {
			if row.core && !core[row.name] {
				a.invalid("$.spec.ceph.services", "managed data-bearing Ceph requires explicit MON, MGR and OSD services")
			}
		}
	}
	if !partial {
		a.nativeAllocationConflicts(ceph)
		a.nativeServiceGraph(ceph)
	}
	a.nativeBindings(ceph, partial)
}

type refusedMember struct {
	names           []string
	message, remedy string
}

func (a *admission) withoutRefusedMembers(spec api.Value, typeName, field string) api.Value {
	table := map[string][]refusedMember{"rgw": rgwRefusedMembers, "ingress": ingressRefusedMembers, "nfs": nfsRefusedMembers}[typeName]
	if spec.Type() != api.Mapping {
		return spec
	}
	for _, member := range spec.Fields() {
		for _, row := range table {
			if slices.Contains(row.names, member.Name) {
				a.issue("api.invariant", field+"."+member.Name, row.message, strings.ReplaceAll(row.remedy, "<member>", member.Name))
				spec = spec.Without(member.Name)
			}
		}
	}
	return spec
}

func (a *admission) nativeFamily(service api.Value, typeName, field string, placed []api.Value, partial bool) {
	switch typeName {
	case "rgw":
		a.rgwRecord(service, field, partial)
	case "ingress":
		a.ingressRecord(service, field, placed, partial)
	case "nfs":
		a.nfsRecord(service, field)
	}
}

func (a *admission) nativeBindings(ceph api.Value, partial bool) {
	refs := map[string]bool{}
	for i, binding := range ceph.Get("serviceBindings").Items() {
		field := indexed("$.spec.ceph.serviceBindings", i)
		ref := textOf(binding, "serviceRef")
		if refs[ref] {
			a.invalid(field+".serviceRef", "service binding identities must be unique")
		}
		refs[ref] = true
		service, found := NativeService(a.object, ref)
		if !partial && !found {
			a.invalid(field+".serviceRef", "service binding must resolve to one declared native service")
		}
		refusal := ""
		if found {
			refusal = noBindingRefusal(a.object, service)
		}
		if refusal != "" {
			a.issue("api.invariant", field+".serviceRef", refusal, "remove this spec.ceph.serviceBindings entry")
		}
		paths := []string{}
		for j, secret := range binding.Get("secrets").Items() {
			pointer := textOf(secret, "path")
			leaf := indexed(field+".secrets", j)
			if !validSecretPointer(pointer) {
				a.invalid(leaf+".path", "secret binding path must be a non-root exact JSON Pointer")
			}
			for _, earlier := range paths {
				if earlier == pointer || strings.HasPrefix(earlier, pointer+"/") || strings.HasPrefix(pointer, earlier+"/") {
					a.invalid(leaf+".path", "secret binding paths cannot duplicate or overlap")
				}
			}
			paths = append(paths, pointer)
		}
		if refusal == "" && (found || !partial) {
			a.bindingLeaves(binding, service, field, partial)
		}
	}
}

func validSecretPointer(pointer string) bool {
	if !strings.HasPrefix(pointer, "/") || pointer == "/" || strings.ContainsAny(pointer, "\x00\r\n") {
		return false
	}
	for _, token := range strings.Split(pointer[1:], "/") {
		if token == "" || token == "-" || strings.Contains(token, "*") {
			return false
		}
		for i := 0; i < len(token); i++ {
			if token[i] == '~' {
				if i+1 == len(token) || (token[i+1] != '0' && token[i+1] != '1') {
					return false
				}
				i++
			}
		}
	}
	return true
}

var retiredResourceFields = map[api.Kind]map[string]string{
	api.StorageFilesystem: {
		"$.spec.mds.placement":   "an entry of its StorageCluster's spec.ceph.services with service_type mds and service_id equal to this StorageFilesystem's name, and set spec.mdsServiceRef to mds.<name>",
		"$.spec.mds.serviceSpec": "the native fields (networks, extra_container_args, extra_entrypoint_args) of its StorageCluster's spec.ceph.services entry with service_type mds and service_id equal to this StorageFilesystem's name",
	},
}

func retiredIssue(field, replacement string) api.Issue {
	return api.Issue{Code: "api.field", Field: field, Message: "storage daemon helper was retired by the native-service migration", Remediation: "replace " + field + " with " + replacement}
}

func RetiredField(kind api.Kind, field string) (api.Issue, bool) {
	if kind != api.StorageCluster {
		replacement, ok := retiredResourceFields[kind][field]
		if !ok {
			return api.Issue{}, false
		}
		return retiredIssue(field, replacement), true
	}
	replacement := ""
	switch field {
	case "$.spec.ceph.monitoring", "$.spec.ceph.mgmtGateway":
		replacement = "$.spec.ceph.services"
	case "$.spec.ceph.topology.osdDrivegroups":
		replacement = "$.spec.ceph.services[].spec.data_devices"
	default:
		if rest := strings.TrimPrefix(field, "$.spec.ceph.services["); rest != field {
			index, tail, ok := strings.Cut(rest, "]")
			if ok && index != "" && strings.IndexFunc(index, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
				switch tail {
				case ".serviceType":
					replacement = strings.TrimSuffix(field, ".serviceType") + ".service_type"
				case ".serviceID":
					replacement = strings.TrimSuffix(field, ".serviceID") + ".service_id"
				}
			}
			if replacement != "" {
				break
			}
		}
		rest := strings.TrimPrefix(field, "$.spec.ceph.topology.nodes[")
		if rest == field {
			return api.Issue{}, false
		}
		index, tail, ok := strings.Cut(rest, "]")
		if !ok || index == "" || strings.IndexFunc(index, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return api.Issue{}, false
		}
		if tail == ".roles" || tail == ".devices" || tail == ".osd" {
			replacement = "$.spec.ceph.services"
		}
	}
	if replacement == "" {
		return api.Issue{}, false
	}
	return retiredIssue(field, replacement), true
}

func (a *admission) retired(field string) {
	if issue, ok := RetiredField(a.object.Kind(), field); ok {
		a.issue(issue.Code, issue.Field, issue.Message, issue.Remediation)
	}
}

func (a *admission) retiredCluster(ceph api.Value) {
	for _, key := range []string{"monitoring", "mgmtGateway"} {
		if ceph.Has(key) {
			a.retired("$.spec.ceph." + key)
		}
	}
	if ceph.Has("topology", "osdDrivegroups") {
		a.retired("$.spec.ceph.topology.osdDrivegroups")
	}
	for i, node := range ceph.Get("topology", "nodes").Items() {
		for _, key := range []string{"roles", "devices", "osd"} {
			if node.Has(key) {
				a.retired(indexed("$.spec.ceph.topology.nodes", i) + "." + key)
			}
		}
	}
}
