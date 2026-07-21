package desiredstate

import (
	"fmt"
	"net/url"
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
		errs = append(errs, validateEntitlementType(owner, entitlement)...)
		errs = append(errs, validateEntitlementRegistry(owner+".registry", entitlement.Spec.Registry)...)
	}
	return errs
}

func validateEntitlementType(owner string, entitlement v1alpha1.Entitlement) []string {
	spec := entitlement.Spec
	switch spec.Type {
	case v1alpha1.EntitlementTypeRedHatRHEL:
		return validateEntitlementRHSMRequired(owner+".rhsm", spec.RHSM)
	case v1alpha1.EntitlementTypeRedHatCeph:
		errs := validateEntitlementRHSMRequired(owner+".rhsm", spec.RHSM)
		return append(errs, validateEntitlementRegistryCredentialsRequired(owner+".registry", spec.Registry)...)
	case v1alpha1.EntitlementTypeIBMStorageCeph:
		errs := validateEntitlementRegistryCredentialsRequired(owner+".registry", spec.Registry)
		if spec.License == nil || !spec.License.Accept {
			errs = append(errs, owner+".license.accept must be true for ibm-storage-ceph")
		}
		if spec.RHSM != nil {
			errs = append(errs, owner+".rhsm is not allowed for ibm-storage-ceph; register the RHEL OS via a MachineInstallProfile spec.subscription that references a redhat-rhel entitlement")
		}
		return errs
	case "":
		return []string{owner + ".type is required"}
	default:
		return []string{fmt.Sprintf("%s.type %q must be one of {%s}", owner, spec.Type, strings.Join(v1alpha1.EntitlementTypes(), ", "))}
	}
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
		value := registry.URL
		if strings.ContainsAny(value, " \t\r\n") {
			errs = append(errs, owner+".url must not contain whitespace")
		}
		if proxyURLHasInlineCredentials(value) || strings.Contains(value, "@") {
			errs = append(errs, owner+".url must not embed credentials; use credentialsRef")
		}
		if strings.Contains(value, "://") {
			errs = append(errs, owner+".url must be a scheme-less registry address, not a URL")
		}
		parsed, err := url.Parse("//" + value)
		if err != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
			errs = append(errs, owner+".url must be host[:port][/path] with no scheme, query, fragment, or trailing slash")
		} else {
			for _, segment := range strings.Split(strings.TrimPrefix(parsed.EscapedPath(), "/"), "/") {
				if segment == "" && parsed.Path != "" || segment == "." || segment == ".." {
					errs = append(errs, owner+".url path must contain non-empty registry namespace segments")
					break
				}
			}
		}
	}
	return errs
}
