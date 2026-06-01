package render

import "github.com/crmarques/bootwright/api/v1alpha1"

func clusterMachineNetworkAttachmentVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, m v1alpha1.ClusterMachineComponent) map[string]any {
	binding, ok := findClusterNetworkBinding(ci, m.From.Provider, m.NetworkConfig.Ref.Name)
	if !ok {
		return nil
	}
	provider, ok := findProvider(state, m.From.Provider)
	if !ok {
		return nil
	}
	attachment, ok := findNetworkAttachment(provider, binding.AttachmentRef.Name)
	if !ok {
		return nil
	}
	return networkAttachmentVars(attachment)
}

func clusterNetworkAttachmentVars(state v1alpha1.State, ci v1alpha1.ClusterInfra, networkName string) map[string]any {
	var first map[string]any
	for _, binding := range ci.Spec.NetworkBindings {
		if binding.NetworkConfigRef.Name != networkName {
			continue
		}
		provider, ok := findProvider(state, binding.ProviderRef.Name)
		if !ok {
			continue
		}
		attachment, ok := findNetworkAttachment(provider, binding.AttachmentRef.Name)
		if !ok {
			continue
		}
		vars := networkAttachmentVars(attachment)
		if first == nil {
			first = vars
		}
		if vars["kind"] == v1alpha1.ProvisionerLibvirt {
			return vars
		}
	}
	return first
}

func networkAttachmentVars(attachment v1alpha1.NetworkAttachmentCapability) map[string]any {
	out := map[string]any{}
	switch {
	case attachment.Libvirt != nil:
		out["kind"] = v1alpha1.ProvisionerLibvirt
		out["libvirt"] = map[string]any{"bridge": attachment.Libvirt.Bridge}
	case attachment.VSphere != nil:
		out["kind"] = v1alpha1.ProvisionerVSphere
		out["vsphere"] = map[string]any{"portgroup": attachment.VSphere.Portgroup}
	case attachment.KubeVirt != nil:
		out["kind"] = v1alpha1.ProvisionerKubeVirt
		out["kubevirt"] = map[string]any{"nad": kubeVirtNADName(attachment.KubeVirt.NADRef)}
	case attachment.BareMetal != nil:
		out["kind"] = v1alpha1.ProvisionerBareMetal
		baremetal := map[string]any{}
		if attachment.BareMetal.VLAN != 0 {
			baremetal["vlan"] = attachment.BareMetal.VLAN
		}
		out["baremetal"] = baremetal
	}
	return out
}

func kubeVirtNADName(ref v1alpha1.KubeVirtNADReference) string {
	if ref.Namespace == "" {
		return ref.Name
	}
	return ref.Namespace + "/" + ref.Name
}
