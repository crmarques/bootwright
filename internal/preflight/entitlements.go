package preflight

import "github.com/crmarques/bootwright/api/v1alpha1"

func collectEntitlementSecretRefRequirements(state v1alpha1.State) []secretRefRequirement {
	var out []secretRefRequirement
	appendEntitlement := func(refName, label string, rhsmPhases, registryPhases []string, owner secretRefOwner) {
		entitlement, ok := v1alpha1.EntitlementByName(state.Entitlements, refName)
		if !ok {
			return
		}
		rhsm := v1alpha1.EntitlementRHSMByName(state.Entitlements, refName)
		if rhsm != nil && v1alpha1.EntitlementRHSMManagement(rhsm) == v1alpha1.EntitlementRHSMManagementManaged {
			if rhsm.OrganizationRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: rhsm.OrganizationRef.Name,
					label:   label + " rhsm organizationRef",
					phases:  rhsmPhases,
					owner:   owner,
				})
			}
			if rhsm.ActivationKeyRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: rhsm.ActivationKeyRef.Name,
					label:   label + " rhsm activationKeyRef",
					phases:  rhsmPhases,
					owner:   owner,
				})
			}
			if rhsm.Satellite != nil && rhsm.Satellite.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: rhsm.Satellite.TrustBundleRef.Name,
					label:   label + " rhsm satellite trustBundleRef",
					phases:  rhsmPhases,
					owner:   owner,
				})
			}
		}
		if entitlement.Spec.Registry != nil {
			if entitlement.Spec.Registry.CredentialsRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Spec.Registry.CredentialsRef.Name,
					label:   label + " registry credentialsRef",
					phases:  registryPhases,
					owner:   owner,
				})
			}
			if entitlement.Spec.Registry.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Spec.Registry.TrustBundleRef.Name,
					label:   label + " registry trustBundleRef",
					phases:  registryPhases,
					owner:   owner,
				})
			}
		}
	}
	for _, profile := range state.MachineInstallProfiles {
		if profile.Spec.Installer.Anaconda == nil {
			continue
		}
		cdn := profile.Spec.Installer.Anaconda.PackageSource.GetFromSubscription()
		if cdn == nil || cdn.EntitlementRef.Name == "" {
			continue
		}
		appendEntitlement(
			cdn.EntitlementRef.Name,
			"MachineInstallProfile/"+profile.Metadata.Name+" installer.anaconda.packageSource.fromSubscription entitlementRef",
			[]string{"machines"},
			[]string{"machines"},
			secretRefOwner{},
		)
	}
	for _, profile := range state.MachineInstallProfiles {
		if profile.Spec.Subscription == nil || profile.Spec.Subscription.EntitlementRef.Name == "" {
			continue
		}
		appendEntitlement(
			profile.Spec.Subscription.EntitlementRef.Name,
			"MachineInstallProfile/"+profile.Metadata.Name+" subscription entitlementRef",
			[]string{"machines"},
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
			[]string{"machines"},
			[]string{"deps", "base"},
			secretRefOwner{storageCluster: cluster.Metadata.Name},
		)
	}
	return out
}
