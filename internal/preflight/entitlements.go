package preflight

import "github.com/crmarques/bootwright/api/v1alpha1"

func collectEntitlementSecretRefRequirements(state v1alpha1.State, env *v1alpha1.Environment) []secretRefRequirement {
	entitlementsByName := map[string]v1alpha1.EnvironmentEntitlement{}
	for _, entitlement := range env.Spec.Entitlements {
		entitlementsByName[entitlement.Name] = entitlement
	}
	var out []secretRefRequirement
	appendEntitlement := func(refName, label string, phases []string, owner secretRefOwner) {
		entitlement, ok := entitlementsByName[refName]
		if !ok {
			return
		}
		// rhsm is either inline or, for ibm/ibm-storage-ceph, deferred to a
		// referenced redhat/rhel entitlement; collect its secrets from
		// whichever carries it so they stay required.
		rhsm := entitlement.RHSM
		if rhsm == nil && entitlement.RHELEntitlementRef.Name != "" {
			if rhel, ok := entitlementsByName[entitlement.RHELEntitlementRef.Name]; ok {
				rhsm = rhel.RHSM
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
		}
		if entitlement.Registry != nil {
			if entitlement.Registry.CredentialsRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Registry.CredentialsRef.Name,
					label:   label + " registry credentialsRef",
					phases:  phases,
					owner:   owner,
				})
			}
			if entitlement.Registry.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Registry.TrustBundleRef.Name,
					label:   label + " registry trustBundleRef",
					phases:  phases,
					owner:   owner,
				})
			}
		}
	}
	for _, image := range state.MachineImages {
		if image.Spec.InstallSource.EntitlementRef.Name == "" {
			continue
		}
		appendEntitlement(
			image.Spec.InstallSource.EntitlementRef.Name,
			"MachineImage/"+image.Metadata.Name+" installSource entitlementRef",
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
