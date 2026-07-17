package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateEntitlements(state v1alpha1.State) []string {
	var errs []string
	for _, entitlement := range state.Entitlements {
		if e := validateName(v1alpha1.KindEntitlement, entitlement.Metadata.Name); e != "" {
			errs = append(errs, e)
		}
		owner := fmt.Sprintf("Entitlement/%s spec", entitlement.Metadata.Name)
		errs = append(errs, validateEntitlementType(owner, entitlement, state)...)
		errs = append(errs, validateEntitlementRegistry(owner+".registry", entitlement.Spec.Registry)...)
	}
	return errs
}

func validateEntitlementType(owner string, entitlement v1alpha1.Entitlement, state v1alpha1.State) []string {
	spec := entitlement.Spec
	switch spec.Type {
	case v1alpha1.EntitlementTypeRedHatRHEL:
		errs := validateEntitlementRHSMRequired(owner+".rhsm", spec.RHSM)
		return append(errs, rejectRHELEntitlementRef(owner, spec)...)
	case v1alpha1.EntitlementTypeRedHatCeph:
		errs := validateEntitlementRHSMRequired(owner+".rhsm", spec.RHSM)
		errs = append(errs, validateEntitlementRegistryCredentialsRequired(owner+".registry", spec.Registry)...)
		return append(errs, rejectRHELEntitlementRef(owner, spec)...)
	case v1alpha1.EntitlementTypeIBMStorageCeph:
		errs := validateEntitlementRegistryCredentialsRequired(owner+".registry", spec.Registry)
		if spec.License == nil || !spec.License.Accept {
			errs = append(errs, owner+".license.accept must be true for ibm-storage-ceph")
		}
		if spec.RHSM != nil {
			errs = append(errs, owner+".rhsm is not allowed for ibm-storage-ceph; reference a redhat-rhel entitlement via rhelEntitlementRef for the RHEL subscription")
		}
		return append(errs, validateEntitlementRHELRef(owner+".rhelEntitlementRef", spec.RHELEntitlementRef, state)...)
	case "":
		return []string{owner + ".type is required"}
	default:
		return []string{fmt.Sprintf("%s.type %q must be one of {%s}", owner, spec.Type, strings.Join(v1alpha1.EntitlementTypes(), ", "))}
	}
}

func rejectRHELEntitlementRef(owner string, spec v1alpha1.EntitlementSpec) []string {
	if spec.RHELEntitlementRef.Name != "" {
		return []string{owner + ".rhelEntitlementRef is only valid for the ibm-storage-ceph type"}
	}
	return nil
}

func validateEntitlementRHELRef(owner string, ref v1alpha1.LocalObjectReference, state v1alpha1.State) []string {
	if ref.Name == "" {
		return []string{owner + " is required for ibm-storage-ceph; name a redhat-rhel entitlement for the RHEL subscription"}
	}
	target, ok := v1alpha1.EntitlementByName(state.Entitlements, ref.Name)
	if !ok {
		return []string{fmt.Sprintf("%s %q does not match any Entitlement", owner, ref.Name)}
	}
	if target.Spec.Type != v1alpha1.EntitlementTypeRedHatRHEL {
		return []string{fmt.Sprintf("%s %q resolves to type %q, want %q", owner, ref.Name, target.Spec.Type, v1alpha1.EntitlementTypeRedHatRHEL)}
	}
	return nil
}

func validateEntitlementRHSMRequired(owner string, rhsm *v1alpha1.EntitlementRHSM) []string {
	if rhsm == nil {
		return []string{owner + " is required"}
	}
	switch rhsm.Management {
	case "", v1alpha1.EntitlementRHSMManagementManaged:
	case v1alpha1.EntitlementRHSMManagementExternal:
		return validateEntitlementRHSMExternal(owner, rhsm)
	default:
		return []string{fmt.Sprintf("%s.management %q must be one of {%s, %s}", owner, rhsm.Management, v1alpha1.EntitlementRHSMManagementManaged, v1alpha1.EntitlementRHSMManagementExternal)}
	}
	var errs []string
	if rhsm.OrganizationRef.Name == "" {
		errs = append(errs, owner+".organizationRef is required")
	}
	if rhsm.ActivationKeyRef.Name == "" {
		errs = append(errs, owner+".activationKeyRef is required")
	}
	return append(errs, validateEntitlementSatellite(owner+".satellite", rhsm.Satellite)...)
}

func validateEntitlementRHSMExternal(owner string, rhsm *v1alpha1.EntitlementRHSM) []string {
	var errs []string
	if rhsm.OrganizationRef.Name != "" {
		errs = append(errs, owner+".organizationRef must be unset when management is external; the operator provisioning playbook owns registration")
	}
	if rhsm.ActivationKeyRef.Name != "" {
		errs = append(errs, owner+".activationKeyRef must be unset when management is external; the operator provisioning playbook owns registration")
	}
	if rhsm.Satellite != nil {
		errs = append(errs, owner+".satellite must be unset when management is external; the operator provisioning playbook owns Satellite trust and registration")
	}
	if rhsm.ConnectToInsights {
		errs = append(errs, owner+".connectToInsights must be unset when management is external; the operator provisioning playbook owns Insights registration")
	}
	return errs
}

func validateEntitlementSatellite(owner string, sat *v1alpha1.EntitlementRHSMSatellite) []string {
	if sat == nil {
		return nil
	}
	var errs []string
	switch {
	case strings.TrimSpace(sat.Hostname) == "":
		errs = append(errs, owner+".hostname is required when satellite is set")
	case sat.Hostname != strings.TrimSpace(sat.Hostname):
		errs = append(errs, owner+".hostname must not contain leading or trailing whitespace")
	case strings.Contains(sat.Hostname, "://") || strings.ContainsAny(sat.Hostname, "/ \t\r\n"):
		errs = append(errs, owner+".hostname must be a bare host (no scheme or path), not a URL")
	}
	if sat.ContentBaseURL != "" {
		if err := validateHTTPURL(sat.ContentBaseURL); err != nil {
			errs = append(errs, fmt.Sprintf("%s.contentBaseURL %q is invalid: %v", owner, sat.ContentBaseURL, err))
		}
	}
	return errs
}

func validateEntitlementRegistryCredentialsRequired(owner string, registry *v1alpha1.EntitlementRegistry) []string {
	if registry == nil || registry.CredentialsRef.Name == "" {
		return []string{owner + ".credentialsRef is required"}
	}
	return nil
}

func validateEntitlementRegistry(owner string, registry *v1alpha1.EntitlementRegistry) []string {
	if registry == nil {
		return nil
	}
	var errs []string
	if registry.URL != "" {
		if strings.ContainsAny(registry.URL, " \t\r\n") {
			errs = append(errs, owner+".url must not contain whitespace")
		}
		if proxyURLHasInlineCredentials(registry.URL) || strings.Contains(registry.URL, "@") {
			errs = append(errs, owner+".url must not embed credentials; use credentialsRef")
		}
	}
	return errs
}
