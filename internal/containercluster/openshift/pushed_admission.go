package openshift

import (
	"fmt"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate"
)

func validatePushedNodes(cluster api.Object, catalog api.Catalog) []api.Issue {
	arms, complete := providerTypes(cluster, catalog)
	if !complete || !slices.Contains(arms, substrate.ArmKubeVirt) {
		return nil
	}
	var issues []api.Issue
	if len(arms) != 1 {
		issues = append(issues, invariantRemediation("$.spec.nodes", "KubeVirt cluster nodes cannot be mixed with another substrate arm", "place every node of this cluster on a KubeVirt provider"))
	}
	if len(arms) == 1 && cluster.Spec().Has("install", "agent", "redfishVirtualMedia") {
		issues = append(issues, invariantRemediation("$.spec.install.agent.redfishVirtualMedia", "a push-only cluster publishes no Redfish virtual media", "remove spec.install.agent.redfishVirtualMedia from this cluster"))
	}
	baremetal := Platform(cluster.Spec()) == "baremetal"
	if baremetal && cluster.Spec().Get("nodes").Len() > 1 {
		for _, slot := range endpointSlots {
			if sourceType(cluster.Spec().Get("install", "endpoints", slot)) != "installer" {
				issues = add(issues, invariantRemediation("$.spec.install.endpoints."+slot+".source.type", "a KubeVirt baremetal platform requires installer endpoint sources", "set spec.install.endpoints."+slot+".source.type to installer"))
			}
		}
	}
	for index, node := range cluster.Spec().Get("nodes").Items() {
		bound, _ := catalog.Find(api.Machine, node.Get("machineRef").Text())
		provider, ok := machine.Provider(bound, catalog)
		if !ok || substrate.Variant(provider) != substrate.ArmKubeVirt {
			continue
		}
		path := fmt.Sprintf("$.spec.nodes[%d].machineRef", index)
		if !baremetal {
			continue
		}
		network := bound.Spec().Get("network")
		references := []string{network.Get("attachmentRef").Text()}
		if attachments := substrate.MachineInterfaceAttachments(bound); attachments.Present() {
			references = nil
			for _, attached := range attachments.Items() {
				references = append(references, attached.Get("attachmentRef").Text())
			}
		}
		for _, name := range references {
			kind := ""
			for _, attachment := range provider.Spec().Get("networkAttachments").Items() {
				if attachment.Get("name").Text() == name {
					kind = attachment.Get("kubevirt", "networkRef", "kind").Text()
				}
			}
			if kind != "ClusterUserDefinedNetwork" && kind != "NetworkAttachmentDefinition" {
				issues = add(issues, reference(path, "a KubeVirt baremetal platform requires ClusterUserDefinedNetwork or NetworkAttachmentDefinition attachments", "select a Localnet ClusterUserDefinedNetwork or bridge NetworkAttachmentDefinition for every interface of "+bound.Identity()))
			}
		}
	}
	return issues
}
