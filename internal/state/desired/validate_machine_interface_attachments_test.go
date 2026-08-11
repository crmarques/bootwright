package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestMachineInterfaceAttachmentsRequireExactKubeVirtInterfaceCoverage(t *testing.T) {
	provider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "child-kubevirt"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{
				{Name: "machine", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{}},
				{Name: "ceph-public", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{}},
			},
		},
	}
	effective := map[string]any{"interfaces": []any{
		map[string]any{
			"name": "bond0",
			"type": "bond",
			"link-aggregation": map[string]any{
				"port": []any{"primary", "ceph-public"},
			},
		},
		map[string]any{"name": "ceph-public.200", "type": "vlan"},
	}}
	config := v1alpha1.MachineNetworkConfig{
		Spec: &v1alpha1.NetworkConfigSpec{},
		InterfaceAttachments: []v1alpha1.MachineInterfaceAttachment{
			{Interface: "primary", AttachmentRef: v1alpha1.LocalObjectReference{Name: "machine"}},
			{Interface: "ceph-public", AttachmentRef: v1alpha1.LocalObjectReference{Name: "ceph-public"}},
		},
	}
	if errs := validateMachineInterfaceAttachments("Machine/node spec.network.config.interfaceAttachments", config, provider, effective); len(errs) != 0 {
		t.Fatalf("valid interface attachments: %v", errs)
	}

	missing := config
	missing.InterfaceAttachments = missing.InterfaceAttachments[:1]
	if errs := validateMachineInterfaceAttachments("bindings", missing, provider, effective); !containsSubstring(errs, `must bind physical interface "ceph-public"`) {
		t.Fatalf("missing coverage errors = %v", errs)
	}

	duplicate := config
	duplicate.InterfaceAttachments = append(append([]v1alpha1.MachineInterfaceAttachment(nil), config.InterfaceAttachments...), config.InterfaceAttachments[0])
	if errs := validateMachineInterfaceAttachments("bindings", duplicate, provider, effective); !containsSubstring(errs, `interface "primary" is duplicated`) {
		t.Fatalf("duplicate errors = %v", errs)
	}

	virtual := config
	virtual.InterfaceAttachments = append(append([]v1alpha1.MachineInterfaceAttachment(nil), config.InterfaceAttachments...), v1alpha1.MachineInterfaceAttachment{
		Interface: "ceph-public.200", AttachmentRef: v1alpha1.LocalObjectReference{Name: "ceph-public"},
	})
	if errs := validateMachineInterfaceAttachments("bindings", virtual, provider, effective); !containsSubstring(errs, `interface "ceph-public.200" does not match any physical interface`) {
		t.Fatalf("virtual interface errors = %v", errs)
	}
}

func TestMachineInterfaceAttachmentsResolveKubeVirtAttachments(t *testing.T) {
	provider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "child-kubevirt"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{
				{Name: "machine", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{}},
				{Name: "wrong-kind", Libvirt: &v1alpha1.NetworkAttachmentLibvirt{}},
			},
		},
	}
	effective := map[string]any{"interfaces": []any{map[string]any{"name": "primary", "type": "ethernet"}}}
	config := v1alpha1.MachineNetworkConfig{
		Spec: &v1alpha1.NetworkConfigSpec{},
		InterfaceAttachments: []v1alpha1.MachineInterfaceAttachment{{
			Interface: "primary", AttachmentRef: v1alpha1.LocalObjectReference{Name: "missing"},
		}},
	}
	if errs := validateMachineInterfaceAttachments("bindings", config, provider, effective); !containsSubstring(errs, `attachmentRef "missing" does not match any networkAttachments[]`) {
		t.Fatalf("missing attachment errors = %v", errs)
	}

	config.InterfaceAttachments[0].AttachmentRef.Name = "wrong-kind"
	if errs := validateMachineInterfaceAttachments("bindings", config, provider, effective); !containsSubstring(errs, `interfaceAttachments require kubevirt attachments`) {
		t.Fatalf("kind mismatch errors = %v", errs)
	}

	provider.Spec.Type = v1alpha1.ProvisionerLibvirt
	if errs := validateMachineInterfaceAttachments("bindings", config, provider, effective); !containsSubstring(errs, `only supported on a kubevirt InfraProvider`) {
		t.Fatalf("provider type errors = %v", errs)
	}
}

func TestMachineNetworkAttachmentFormsAreMutuallyExclusive(t *testing.T) {
	provider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "child-kubevirt"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerKubeVirt,
			NetworkAttachments: []v1alpha1.NetworkAttachmentCapability{{
				Name: "machine", KubeVirt: &v1alpha1.NetworkAttachmentKubeVirt{},
			}},
		},
	}
	network := v1alpha1.NetworkConfig{
		Metadata: v1alpha1.Metadata{Name: "child"},
		Spec: v1alpha1.NetworkConfigSpec{Template: v1alpha1.NetworkConfigTemplate{NetworkConfig: map[string]any{
			"interfaces": []any{map[string]any{"name": "primary", "type": "ethernet"}},
		}}},
	}
	machine := v1alpha1.Machine{Spec: v1alpha1.MachineSpec{
		Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: "child-kubevirt"}},
		Network: v1alpha1.MachineNetwork{Config: v1alpha1.MachineNetworkConfig{
			NetworkConfigRef: v1alpha1.LocalObjectReference{Name: "child"},
			AttachmentRef:    v1alpha1.LocalObjectReference{Name: "machine"},
			InterfaceAttachments: []v1alpha1.MachineInterfaceAttachment{{
				Interface: "primary", AttachmentRef: v1alpha1.LocalObjectReference{Name: "machine"},
			}},
		}},
	}}
	errs := validateMachineNetwork("Machine/node spec.network", machine, map[string]v1alpha1.NetworkConfig{"child": network}, provider)
	if !containsSubstring(errs, "must set only one of attachmentRef or interfaceAttachments") {
		t.Fatalf("mutual-exclusion errors = %v", errs)
	}
}
