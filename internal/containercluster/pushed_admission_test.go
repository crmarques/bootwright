package containercluster

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func kubevirtAttachments(kind string) (api.Object, api.Catalog) {
	cluster, catalog := fixture("kubevirt", 3, false)
	cluster = cluster.WithSpec(cluster.Spec().WithPath(m("type", "baremetal"), "install", "platform"))
	for _, slot := range endpointSlots {
		cluster = cluster.WithSpec(cluster.Spec().WithPath(m("type", "openshift"), "install", "endpoints", slot, "source"))
	}
	objects := catalog.Objects()
	for index, object := range objects {
		spec := object.Spec()
		if object.Kind() == api.InfraProvider {
			spec = spec.With("networkAttachments", list(m("name", "lan", "kubevirt", m("networkRef", m("kind", kind, "name", "lan")))))
		}
		if object.Kind() == api.Machine {
			spec = spec.WithPath(api.StringValue("lan"), "network", "attachmentRef")
		}
		objects[index] = object.WithSpec(spec)
	}
	return cluster, api.NewCatalog(objects)
}

func TestKubeVirtBaremetalAttachmentKinds(t *testing.T) {
	for _, kind := range []string{"ClusterUserDefinedNetwork", "NetworkAttachmentDefinition", "UserDefinedNetwork", "Other"} {
		cluster, catalog := kubevirtAttachments(kind)
		issues := validatePushedNodes(cluster, catalog)
		if allowed := kind == "ClusterUserDefinedNetwork" || kind == "NetworkAttachmentDefinition"; allowed != (len(issues) == 0) {
			t.Fatalf("%s: %v", kind, issues)
		}
		if len(issues) != 0 && issues[0].Field != "$.spec.nodes[0].machineRef" {
			t.Fatal(issues)
		}
	}
}

func TestKubeVirtClusterRefusesFetchSelectionAndMixedArms(t *testing.T) {
	cluster, catalog := fixture("kubevirt", 3, false)
	fetch := cluster.WithSpec(cluster.Spec().WithPath(m(), "install", "agent", "redfishVirtualMedia"))
	if issues := Validate(fetch, catalog); !hasIssue(issues, "$.spec.install.agent.redfishVirtualMedia", "push-only") {
		t.Fatal(issues)
	}
	objects := catalog.Objects()
	for index, object := range objects {
		if object.Kind() == api.Machine && object.Name() == "node-0" {
			objects[index] = object.WithSpec(object.Spec().WithPath(api.StringValue("other"), "substrate", "providerRef"))
		}
	}
	objects = append(objects, obj(api.InfraProvider, "other", m("libvirt", m())))
	issues := Validate(cluster, api.NewCatalog(objects))
	if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
		return issue.Field == "$.spec.nodes" && strings.Contains(issue.Message, "cannot be mixed")
	}) {
		t.Fatal(issues)
	}
}

func TestKubeVirtBaremetalRefusesExternalEndpointSources(t *testing.T) {
	cluster, catalog := kubevirtAttachments("NetworkAttachmentDefinition")
	for _, slot := range endpointSlots {
		changed := cluster.WithSpec(cluster.Spec().WithPath(api.StringValue("external"), "install", "endpoints", slot, "source", "type"))
		if issues := Validate(changed, catalog); !hasIssue(issues, "$.spec.install.endpoints."+slot+".source.type", "requires openshift") {
			t.Fatal(issues)
		}
	}
}
