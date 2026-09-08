package environment

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(object api.Object, catalog api.Catalog) (api.Object, []api.Issue) {
	if object.Kind() != api.Environment {
		return object, nil
	}
	spec := object.Spec()
	domains := spec.Get("domains")
	if domains.Has("base") {
		domains = domains.Default("machines", domains.Get("base")).Default("clusters", domains.Get("base"))
		domains = domains.Default("containerClusters", domains.Get("clusters")).Default("storageClusters", domains.Get("clusters"))
		spec = spec.With("domains", domains)
	}
	if proxy := spec.Get("proxy"); proxy.Present() {
		fallback := api.MapValue(api.FieldValue{Name: "direct", Value: api.MapValue()})
		if proxy.Has("defaultRef") {
			fallback = api.MapValue(api.FieldValue{Name: "proxyRef", Value: proxy.Get("defaultRef")})
		}
		for _, consumer := range []string{"bootwright", "containerClusterInstall", "machineOSInstall"} {
			proxy = proxy.Default(consumer, fallback)
		}
		spec = spec.With("proxy", proxy)
	}
	return object.WithSpec(spec), nil
}

func ValidateAuthored(api.Object, api.Catalog) []api.Issue { return nil }

func Validate(object api.Object, catalog api.Catalog) []api.Issue {
	if object.Kind() != api.Environment {
		return nil
	}
	issues := []api.Issue{}
	add := func(field, message string) {
		if len(issues) < 999 {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec." + field, Message: message})
		}
	}
	spec := object.Spec()
	fleetKey := spec.Get("remoteMachinesAccessKey", "keyRef")
	for _, machine := range catalog.OfKind(api.Machine) {
		if machine.Spec().Has("os", "installProfileRef") && !fleetKey.Present() {
			add("remoteMachinesAccessKey.keyRef", "managed OS installation requires a fleet SSH key reference")
		}
		if fleetKey.Present() && !machine.Spec().Has("os", "installProfileRef") && machine.Spec().Get("access", "ssh", "auth", "privateKeyRef").Equal(fleetKey) {
			add("remoteMachinesAccessKey.keyRef", "fleet install key must differ from authored Machine access keys")
		}
	}
	catalogs := spec.Get("infraComponents")
	for _, entry := range []struct {
		catalog, arm string
		external     []string
	}{{"proxies", "proxy", []string{"connection"}}, {"nameResolution", "nameResolution", []string{"address"}}, {"artifactServers", "artifactServer", []string{"endpoints"}}, {"registries", "registry", []string{"url"}}, {"ntp", "ntp", []string{"address"}}} {
		defaults := 0
		for i, row := range catalogs.Get(entry.catalog).Items() {
			base := fmt.Sprintf("infraComponents.%s[%d]", entry.catalog, i)
			if row.Get("name").Text() == "none" {
				add(base+".name", "catalog name is reserved")
			}
			if row.Get("default").Bool() {
				defaults++
			}
			switch row.Get("management").Text() {
			case "managed":
				if !row.Has("componentRef") {
					add(base+".componentRef", "managed catalog entry requires a component reference")
				}
				for _, field := range entry.external {
					if row.Has(field) {
						add(base+"."+field, "managed catalog entries forbid external connection facts")
					}
				}
				if component, ok := catalog.Find(api.InfraComponent, row.Get("componentRef").Text()); ok {
					if !component.Spec().Has(entry.arm) {
						add(base+".componentRef", "component has the wrong service implementation arm")
					}
					if row.Has("endpointRef") && !hasNamed(component.Spec().Get(entry.arm, "endpoints"), row.Get("endpointRef").Text()) {
						add(base+".endpointRef", "endpoint reference does not resolve on the component")
					}
				}
			case "external":
				if row.Has("componentRef") || row.Has("endpointRef") {
					add(base, "external catalog entries forbid managed component references")
				}
				for _, field := range entry.external {
					if !row.Has(field) {
						add(base+"."+field, "external catalog entry requires connection facts")
					}
				}
				if entry.catalog == "proxies" {
					c := row.Get("connection")
					if !c.Has("httpProxy") && !c.Has("httpsProxy") && c.Get("noProxy").Len() == 0 {
						add(base+".connection", "external proxy requires a proxy URL or no-proxy entries")
					}
				}
			}
		}
		if defaults > 1 {
			add("infraComponents."+entry.catalog, "catalog has more than one default entry")
		}
	}
	proxy := spec.Get("proxy")
	for _, field := range []string{"defaultRef", "bootwright.proxyRef", "containerClusterInstall.proxyRef", "machineOSInstall.proxyRef"} {
		value := proxy.Get(strings.Split(field, ".")...)
		if !value.Present() {
			continue
		}
		row, ok := namedValue(catalogs.Get("proxies"), value.Text())
		if !ok {
			add("proxy."+field, "proxy reference does not resolve to one catalog entry")
		} else if field == "machineOSInstall.proxyRef" && row.Get("management").Text() != "external" {
			add("proxy."+field, "machine OS installation requires an external proxy")
		}
	}
	for _, component := range spec.Get("componentImages").Fields() {
		for _, implementation := range component.Value.Fields() {
			if !implementation.Value.Has("local") && !implementation.Value.Has("public") {
				add("componentImages."+component.Name+"."+implementation.Name, "an image pin must supply local or public intent")
			}
		}
	}
	seenSources := map[string]bool{}
	for i, source := range spec.Get("registries", "imageDigestSources").Items() {
		name := source.Get("source").Text()
		if seenSources[name] {
			add(fmt.Sprintf("registries.imageDigestSources[%d].source", i), "image digest sources must be unique")
		}
		seenSources[name] = true
	}
	if rescue := spec.Get("lifecycle", "rescue"); rescue.Present() {
		if version := rescue.Get("os", "version").Text(); version != "" && !strings.HasPrefix(version, "9.") {
			add("lifecycle.rescue.os.version", "rescue requires a RHEL 9 version")
		}
		if image, ok := catalog.Find(api.MachineImage, rescue.Get("imageRef").Text()); ok {
			media := image.Spec().Get("bootMedia").Text()
			if strings.HasPrefix(media, "http") && !image.Spec().Has("checksum") {
				add("lifecycle.rescue.imageRef", "remote rescue media requires a checksum")
			}
		}
		for _, problem := range ValidateArtifactEndpoint(rescue.Get("artifactServerEndpoint"), catalog, true, true) {
			problem.Field = "$.spec.lifecycle.rescue.artifactServerEndpoint" + strings.TrimPrefix(problem.Field, "$")
			issues = append(issues, problem)
		}
	}
	return issues
}

func ValidateArtifactEndpoint(selection api.Value, catalog api.Catalog, managed, persistent bool) []api.Issue {
	if !selection.Present() {
		return nil
	}
	issue := func(field, message string) []api.Issue {
		return []api.Issue{{Code: "api.reference", Field: field, Message: message}}
	}
	envs := catalog.OfKind(api.Environment)
	if len(envs) != 1 {
		return nil
	}
	rows := envs[0].Spec().Get("infraComponents", "artifactServers")
	server := selection.Get("serverRef").Text()
	if server == "" {
		defaults := []api.Value{}
		for _, row := range rows.Items() {
			if row.Get("default").Bool() {
				defaults = append(defaults, row)
			}
		}
		if len(defaults) == 1 {
			server = defaults[0].Get("name").Text()
		} else if len(defaults) == 0 && rows.Len() == 1 {
			server = rows.Items()[0].Get("name").Text()
		} else {
			return issue("$.serverRef", "artifact server selection is missing or ambiguous")
		}
	}
	row, ok := namedValue(rows, server)
	if !ok {
		return issue("$.serverRef", "artifact server reference does not resolve")
	}
	if managed && row.Get("management").Text() != "managed" {
		return issue("$.serverRef", "consumer requires a managed artifact server")
	}
	if row.Get("management").Text() == "external" {
		if !hasNamed(row.Get("endpoints"), selection.Get("endpointRef").Text()) {
			return issue("$.endpointRef", "artifact endpoint reference does not resolve")
		}
		return nil
	}
	component, ok := catalog.Find(api.InfraComponent, row.Get("componentRef").Text())
	if !ok {
		return nil
	}
	arm := component.Spec().Get("artifactServer")
	if !arm.Present() {
		return issue("$.serverRef", "artifact component has the wrong implementation")
	}
	if !hasNamed(arm.Get("endpoints"), selection.Get("endpointRef").Text()) {
		return issue("$.endpointRef", "artifact endpoint reference does not resolve")
	}
	if persistent {
		if retention := arm.Get("retention").Text(); retention != "" && retention != "persistent" {
			return issue("$.serverRef", "rescue requires a persistent artifact server")
		}
		machine, found := catalog.Find(api.Machine, arm.Get("machineRef").Text())
		if found && !machine.Spec().Get("os", "provided").Bool() {
			return issue("$.serverRef", "rescue artifact server requires an OS-ready Machine")
		}
	}
	return nil
}

func namedValue(list api.Value, name string) (api.Value, bool) {
	var found api.Value
	count := 0
	for _, value := range list.Items() {
		if value.Get("name").Text() == name {
			found = value
			count++
		}
	}
	return found, count == 1
}
func hasNamed(list api.Value, name string) bool { _, ok := namedValue(list, name); return ok }

func MachineNetworks(machine api.Object, catalog api.Catalog) []netip.Prefix {
	config := machine.Spec().Get("network", "inline")
	if ref := machine.Spec().Get("network", "configRef").Text(); ref != "" {
		if network, ok := catalog.Find(api.NetworkConfig, ref); ok {
			config = network.Spec()
		}
	}
	result := []netip.Prefix{}
	for _, entry := range config.Get("machineNetwork").Items() {
		if prefix, err := netip.ParsePrefix(entry.Get("cidr").Text()); err == nil {
			result = append(result, prefix.Masked())
		}
	}
	return result
}

func CanonicalCatalog(objects []api.Object) api.Catalog {
	objects = slices.Clone(objects)
	slices.SortStableFunc(objects, func(a, b api.Object) int {
		if rank := api.KindIndex(a.Kind()) - api.KindIndex(b.Kind()); rank != 0 {
			return rank
		}
		return strings.Compare(a.Name(), b.Name())
	})
	return api.NewCatalog(objects)
}
