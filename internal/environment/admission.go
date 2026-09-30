package environment

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func Normalize(object api.Object, _ api.Catalog) (api.Object, []api.Issue) {
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
	return object.WithSpec(spec), nil
}

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
	issues = append(issues, validateController(object, catalog)...)
	if rescue := spec.Get("lifecycle", "rescue"); rescue.Present() {
		// The schema keeps the declaration for a later journey; until one
		// exists, no lifecycle could act on it.
		if len(issues) < 999 {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec.lifecycle.rescue",
				Message:     "no rescue journey exists yet, so no lifecycle can use a rescue declaration",
				Remediation: "remove spec.lifecycle.rescue from " + object.Identity()})
		}
		if version := rescue.Get("os", "version").Text(); version != "" && !strings.HasPrefix(version, "9.") {
			add("lifecycle.rescue.os.version", "rescue requires a RHEL 9 version")
		}
		if image, ok := catalog.Find(api.MachineImage, rescue.Get("imageRef").Text()); ok {
			media := image.Spec().Get("bootMedia").Text()
			if strings.HasPrefix(media, "http") && !image.Spec().Has("checksum") {
				add("lifecycle.rescue.imageRef", "remote rescue media requires a checksum")
			}
		}
		for _, problem := range validateRescueArtifactEndpoint(rescue.Get("artifactServerEndpoint"), catalog) {
			problem.Field = "$.spec.lifecycle.rescue.artifactServerEndpoint" + strings.TrimPrefix(problem.Field, "$")
			issues = append(issues, problem)
		}
	}
	return issues
}

func validateController(environment api.Object, catalog api.Catalog) []api.Issue {
	name := environment.Spec().Get("controller", "machineRef").Text()
	controller, found := catalog.Find(api.Machine, name)
	if !found || !api.ValidLexical("name", name) {
		return nil // Required-field and typed-reference checks own absence.
	}
	issues := []api.Issue{}
	add := func(message string) {
		if len(issues) < 999 {
			issues = append(issues, api.Issue{Code: "api.invariant", Field: "$.spec.controller.machineRef", Message: message})
		}
	}
	if !controller.Spec().Get("os", "provided").Bool() {
		add("controller must reference an OS-ready Machine with os.provided: true")
	}
	if !controller.Spec().Get("access", "local").Bool() || controller.Spec().Has("access", "ssh") {
		add("controller must reference the local Machine with access.local: true and no SSH access")
	}
	if !slices.ContainsFunc(controller.Spec().Get("capabilities").Items(), func(capability api.Value) bool {
		return capability.Text() == "container-runtime"
	}) {
		add("controller Machine must declare the container-runtime capability")
	}
	for _, machine := range catalog.OfKind(api.Machine) {
		if machine.Name() != name && api.ValidLexical("name", machine.Name()) && machine.Spec().Get("access", "local").Bool() {
			add("only the controller Machine may declare local access; Machine/" + machine.Name() + " also declares access.local: true")
		}
	}
	for _, kind := range []api.Kind{api.ContainerCluster, api.StorageCluster} {
		for _, cluster := range catalog.OfKind(kind) {
			if !api.ValidLexical("name", cluster.Name()) {
				continue
			}
			nodes := cluster.Spec().Get("nodes")
			if kind == api.StorageCluster {
				nodes = cluster.Spec().Get("ceph", "topology", "nodes")
			}
			for _, node := range nodes.Items() {
				if node.Get("machineRef").Text() == name {
					add("controller must remain outside cluster node membership; it is a node of " + cluster.Identity())
					break
				}
			}
		}
	}
	return issues
}

func validateRescueArtifactEndpoint(selection api.Value, catalog api.Catalog) []api.Issue {
	if !selection.Present() {
		return nil
	}
	issues := infrastructureservices.ValidateArtifactEndpoint(selection, catalog, "$", false)
	component, ok := catalog.Find(api.ArtifactServer, selection.Get("serverRef").Text())
	if !ok || len(issues) != 0 {
		return issues
	}
	issue := func(message string) []api.Issue {
		return []api.Issue{{Code: "api.invariant", Field: "$.serverRef", Message: message}}
	}
	spec := component.Spec()
	if retention := spec.Get("retention").Text(); retention != "" && retention != "persistent" {
		return issue("rescue requires a persistent artifact server")
	}
	machine, found := catalog.Find(api.Machine, spec.Get("machineRef").Text())
	if found && !machine.Spec().Get("os", "provided").Bool() {
		return issue("rescue artifact server requires an OS-ready Machine")
	}
	return issues
}

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
