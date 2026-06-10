package preflight

import "github.com/crmarques/bootwright/api/v1alpha1"

func collectEntitlementSecretRefRequirements(state v1alpha1.State, env *v1alpha1.Environment) []secretRefRequirement {
	entitlementsByName := map[string]v1alpha1.EnvironmentEntitlement{}
	for _, entitlement := range env.Spec.Entitlements {
		entitlementsByName[entitlement.Name] = entitlement
	}
	var out []secretRefRequirement
	appendEntitlement := func(refName, label string, phases []string) {
		entitlement, ok := entitlementsByName[refName]
		if !ok {
			return
		}
		if entitlement.RHSM != nil {
			if entitlement.RHSM.OrganizationRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.RHSM.OrganizationRef.Name,
					label:   label + " rhsm organizationRef",
					phases:  phases,
				})
			}
			if entitlement.RHSM.ActivationKeyRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.RHSM.ActivationKeyRef.Name,
					label:   label + " rhsm activationKeyRef",
					phases:  phases,
				})
			}
		}
		if entitlement.Registry != nil {
			if entitlement.Registry.CredentialsRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Registry.CredentialsRef.Name,
					label:   label + " registry credentialsRef",
					phases:  phases,
				})
			}
			if entitlement.Registry.TrustBundleRef.Name != "" {
				out = append(out, secretRefRequirement{
					refName: entitlement.Registry.TrustBundleRef.Name,
					label:   label + " registry trustBundleRef",
					phases:  phases,
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
		)
	}
	return out
}
