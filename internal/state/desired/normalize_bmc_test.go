package desiredstate

import (
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func TestApplyBareMetalBMCDefaults(t *testing.T) {
	verifyFalse, verifyTrue := false, true
	bmProvider := v1alpha1.InfraProvider{
		Metadata: v1alpha1.Metadata{Name: "bm"},
		Spec: v1alpha1.InfraProviderSpec{
			Type: v1alpha1.ProvisionerBareMetal,
			BareMetal: &v1alpha1.InfraProviderBareMetal{
				Defaults: v1alpha1.BareMetalDefaultsSpec{
					BMC: &v1alpha1.BMCDefaults{
						TLS: &v1alpha1.BMCTLS{Verify: &verifyFalse},
						VirtualMedia: &v1alpha1.BMCVirtualMedia{
							TLS: &v1alpha1.BMCVirtualMediaTLS{
								Trust:                        v1alpha1.BMCVirtualMediaTrustDisableVerification,
								RestoreVerificationAfterBoot: &verifyFalse,
							},
						},
					},
				},
			},
		},
	}
	addr := "redfish-virtualmedia+https://bmc.test/redfish/v1/Systems/1"
	mk := func(name, providerRef string, bmc v1alpha1.BMCSpec) v1alpha1.Machine {
		return v1alpha1.Machine{
			Metadata: v1alpha1.Metadata{Name: name},
			Spec: v1alpha1.MachineSpec{
				Substrate: v1alpha1.MachineSubstrate{ProviderRef: v1alpha1.LocalObjectReference{Name: providerRef}},
				Hardware:  v1alpha1.MachineHardware{Management: v1alpha1.MachineHardwareManagement{BMC: bmc}},
			},
		}
	}
	creds := v1alpha1.SecretRef{Name: "c"}

	state := v1alpha1.State{
		InfraProviders: []v1alpha1.InfraProvider{
			bmProvider,
			{Metadata: v1alpha1.Metadata{Name: "kv"}, Spec: v1alpha1.InfraProviderSpec{Type: v1alpha1.ProvisionerKubeVirt}},
		},
		Machines: []v1alpha1.Machine{
			mk("inherits", "bm", v1alpha1.BMCSpec{Address: addr, CredentialsRef: creds}),
			mk("overrides", "bm", v1alpha1.BMCSpec{Address: addr, CredentialsRef: creds, TLS: &v1alpha1.BMCTLS{Verify: &verifyTrue}}),
			mk("nobmc", "bm", v1alpha1.BMCSpec{}),
			mk("other", "kv", v1alpha1.BMCSpec{Address: addr}),
		},
	}
	Normalize(&state)

	get := func(name string) v1alpha1.BMCSpec {
		for _, m := range state.Machines {
			if m.Metadata.Name == name {
				return m.Spec.Hardware.Management.BMC
			}
		}
		t.Fatalf("machine %s missing", name)
		return v1alpha1.BMCSpec{}
	}

	in := get("inherits")
	if in.TLS == nil || in.TLS.Verify == nil || *in.TLS.Verify {
		t.Errorf("inherits tls.verify = %v, want false", in.TLS)
	}
	if in.VirtualMedia == nil || in.VirtualMedia.TLS == nil ||
		in.VirtualMedia.TLS.TrustMode() != v1alpha1.BMCVirtualMediaTrustDisableVerification ||
		in.VirtualMedia.TLS.RestoreVerificationEnabled() {
		t.Errorf("inherits virtualMedia = %+v, want disable-verification without restore", in.VirtualMedia)
	}
	in.VirtualMedia.TLS.Trust = v1alpha1.BMCVirtualMediaTrustEstablished
	*in.VirtualMedia.TLS.RestoreVerificationAfterBoot = true
	d := state.InfraProviders[0].Spec.BareMetal.Defaults.BMC.VirtualMedia.TLS
	if d.TrustMode() != v1alpha1.BMCVirtualMediaTrustDisableVerification || d.RestoreVerificationEnabled() {
		t.Errorf("provider default was aliased by machine inheritance")
	}

	if ov := get("overrides"); ov.TLS == nil || ov.TLS.Verify == nil || !*ov.TLS.Verify {
		t.Errorf("overrides tls.verify = %v, want true (node override wins)", ov.TLS)
	}
	if nb := get("nobmc"); nb.TLS != nil || nb.VirtualMedia != nil {
		t.Errorf("nobmc gained BMC defaults: %+v", nb)
	}
	if o := get("other"); o.TLS != nil || o.VirtualMedia != nil {
		t.Errorf("non-baremetal machine gained BMC defaults: %+v", o)
	}
}
