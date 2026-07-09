package preflight

import "github.com/crmarques/bootwright/api/v1alpha1"

func collectEntitlementSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	entitlementsByName := map[string]v1alpha1.Entitlement{}
	for _, entitlement := range state.Entitlements {
		entitlementsByName[entitlement.Metadata.Name] = entitlement
	}
	var out []secretRefRequirement
	appendEntitlement := func(refName, label string, phases []string, owner secretRefOwner) {
		entitlement, ok := entitlementsByName[refName]
		if !ok {
			return
		}
		rhsm := entitlement.Spec.RHSM
		if rhsm == nil && entitlement.Spec.RHELEntitlementRef.Name != "" {
			if rhel, ok := entitlementsByName[entitlement.Spec.RHELEntitlementRef.Name]; ok {
				rhsm = rhel.Spec.RHSM
			}
		}
		if rhsm != nil {
			if rhsm.OrganizationRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: rhsm.OrganizationRef.Name,
					label:   label + " rhsm organizationRef",
					phases:  phases,
					owner:   owner,
				})
			}
			if rhsm.ActivationKeyRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: rhsm.ActivationKeyRef.Name,
					label:   label + " rhsm activationKeyRef",
					phases:  phases,
					owner:   owner,
				})
			}
			if rhsm.Satellite != nil && rhsm.Satellite.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: rhsm.Satellite.TrustBundleRef.Name,
					label:   label + " rhsm satellite trustBundleRef",
					phases:  phases,
					owner:   owner,
				})
			}
		}
		if entitlement.Spec.Registry != nil {
			if entitlement.Spec.Registry.CredentialsRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Spec.Registry.CredentialsRef.Name,
					label:   label + " registry credentialsRef",
					phases:  phases,
					owner:   owner,
				})
			}
			if entitlement.Spec.Registry.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Spec.Registry.TrustBundleRef.Name,
					label:   label + " registry trustBundleRef",
					phases:  phases,
					owner:   owner,
				})
			}
		}
	}
	for _, profile := range state.MachineInstallProfiles {
		if profile.Spec.Installer.Anaconda == nil {
			continue
		}
		cdn := profile.Spec.Installer.Anaconda.PackageSource.GetRedhatCDN()
		if cdn == nil || cdn.EntitlementRef.Name == "" {
			continue
		}
		appendEntitlement(
			cdn.EntitlementRef.Name,
			"MachineInstallProfile/"+profile.Metadata.Name+" installer.anaconda.packageSource.redhatCDN entitlementRef",
			[]string{"machines"},
			secretRefOwner{},
		)
	}
	for _, cluster := range state.StorageClusters {
		if cluster.Spec.Ceph == nil || cluster.Spec.Ceph.EntitlementRef.Name == "" {
			continue
		}
		appendEntitlement(
			cluster.Spec.Ceph.EntitlementRef.Name,
			"StorageCluster/"+cluster.Metadata.Name+" ceph entitlementRef",
			[]string{"deps", "base"},
			secretRefOwner{storageCluster: cluster.Metadata.Name},
		)
	}
	return out
}
