package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
)

func validateEnvironmentEntitlements(env v1alpha1.Environment) []string {
	var errs []string
	seen := map[string]bool{}
	for i, entitlement := range env.Spec.Entitlements {
		owner := fmt.Sprintf("Environment/%s spec.entitlements[%d]", env.Metadata.Name, i)
		errs = append(errs, validateNamedEnvironmentComponent(owner, entitlement.Name, seen)...)
		errs = append(errs, validateEnvironmentEntitlementProviderProduct(owner, entitlement)...)
		errs = append(errs, validateEnvironmentEntitlementRegistry(owner+".registry", entitlement.Registry)...)
		switch entitlement.Product {
		case v1alpha1.EntitlementProductRHEL:
			errs = append(errs, validateEnvironmentEntitlementRHSMRequired(owner+".rhsm", entitlement.RHSM)...)
		case v1alpha1.EntitlementProductCeph:
			if entitlement.Provider == v1alpha1.EntitlementProviderRedHat {
				errs = append(errs, validateEnvironmentEntitlementRHSMRequired(owner+".rhsm", entitlement.RHSM)...)
				errs = append(errs, validateEnvironmentEntitlementRegistryCredentialsRequired(owner+".registry", entitlement.Registry)...)
			}
		case v1alpha1.EntitlementProductIBMStorageCeph:
			errs = append(errs, validateEnvironmentEntitlementRegistryCredentialsRequired(owner+".registry", entitlement.Registry)...)
			if entitlement.License == nil || !entitlement.License.Accept {
				errs = append(errs, owner+".license.accept must be true for IBM Storage Ceph")
			}
			if entitlement.RHSM != nil {
				errs = append(errs, owner+".rhsm is not allowed for IBM Storage Ceph; reference a redhat/rhel entitlement via rhelEntitlementRef for the RHEL subscription")
			}
			errs = append(errs, validateEnvironmentEntitlementRHELRef(owner+".rhelEntitlementRef", entitlement.RHELEntitlementRef, env)...)
		}
		if entitlement.Product != v1alpha1.EntitlementProductIBMStorageCeph && entitlement.RHELEntitlementRef.Name != "" {
			errs = append(errs, owner+".rhelEntitlementRef is only valid for the ibm/ibm-storage-ceph product")
		}
	}
	return errs
}

// validateEnvironmentEntitlementRHELRef checks that an ibm/ibm-storage-ceph
// entitlement's rhelEntitlementRef names an existing redhat/rhel entitlement —
// the RHEL subscription IBM Storage Ceph runs on but does not itself carry.
func validateEnvironmentEntitlementRHELRef(owner string, ref v1alpha1.LocalObjectReference, env v1alpha1.Environment) []string {
	if ref.Name == "" {
		return []string{owner + " is required for IBM Storage Ceph; name a redhat/rhel entitlement for the RHEL subscription"}
	}
	target, ok := entitlements.Find(&env, ref.Name)
	if !ok {
		return []string{fmt.Sprintf("%s %q does not match any Environment.spec.entitlements[].name", owner, ref.Name)}
	}
	if target.Provider != v1alpha1.EntitlementProviderRedHat || target.Product != v1alpha1.EntitlementProductRHEL {
		return []string{fmt.Sprintf("%s %q resolves to %s/%s, want %s/%s", owner, ref.Name, target.Provider, target.Product, v1alpha1.EntitlementProviderRedHat, v1alpha1.EntitlementProductRHEL)}
	}
	return nil
}

func validateEnvironmentEntitlementProviderProduct(owner string, entitlement v1alpha1.EnvironmentEntitlement) []string {
	var errs []string
	switch entitlement.Provider {
	case v1alpha1.EntitlementProviderCommunity, v1alpha1.EntitlementProviderRedHat, v1alpha1.EntitlementProviderIBM:
	case "":
		errs = append(errs, owner+".provider is required")
	default:
		errs = append(errs, fmt.Sprintf("%s.provider %q must be one of {%s, %s, %s}",
			owner, entitlement.Provider, v1alpha1.EntitlementProviderCommunity, v1alpha1.EntitlementProviderRedHat, v1alpha1.EntitlementProviderIBM))
	}
	switch entitlement.Product {
	case v1alpha1.EntitlementProductCeph, v1alpha1.EntitlementProductRHEL, v1alpha1.EntitlementProductOpenShift, v1alpha1.EntitlementProductIBMStorageCeph:
	case "":
		errs = append(errs, owner+".product is required")
	default:
		errs = append(errs, fmt.Sprintf("%s.product %q must be one of {%s, %s, %s, %s}",
			owner, entitlement.Product, v1alpha1.EntitlementProductCeph, v1alpha1.EntitlementProductRHEL, v1alpha1.EntitlementProductOpenShift, v1alpha1.EntitlementProductIBMStorageCeph))
	}
	if entitlement.Provider == "" || entitlement.Product == "" {
		return errs
	}
	if environmentEntitlementProviderProductAllowed(entitlement.Provider, entitlement.Product) {
		return errs
	}
	return append(errs, fmt.Sprintf("%s provider/product %s/%s is not supported", owner, entitlement.Provider, entitlement.Product))
}

func environmentEntitlementProviderProductAllowed(provider, product string) bool {
	switch provider {
	case v1alpha1.EntitlementProviderCommunity:
		return product == v1alpha1.EntitlementProductCeph || product == v1alpha1.EntitlementProductOpenShift
	case v1alpha1.EntitlementProviderRedHat:
		return product == v1alpha1.EntitlementProductCeph || product == v1alpha1.EntitlementProductRHEL || product == v1alpha1.EntitlementProductOpenShift
	case v1alpha1.EntitlementProviderIBM:
		return product == v1alpha1.EntitlementProductIBMStorageCeph
	default:
		return false
	}
}

func validateEnvironmentEntitlementRHSMRequired(owner string, rhsm *v1alpha1.EnvironmentEntitlementRHSM) []string {
	if rhsm == nil {
		return []string{owner + " is required"}
	}
	var errs []string
	if rhsm.OrganizationRef.Name == "" {
		errs = append(errs, owner+".organizationRef is required")
	}
	if rhsm.ActivationKeyRef.Name == "" {
		errs = append(errs, owner+".activationKeyRef is required")
	}
	errs = append(errs, validateEnvironmentEntitlementSatellite(owner+".satellite", rhsm.Satellite)...)
	return errs
}

// validateEnvironmentEntitlementSatellite checks an optional corporate Satellite
// redirect on an rhsm arm: a bare hostname is required when the block is present,
// and contentBaseURL (when set) must be an http(s) URL. The CA secret named by
// trustBundleRef is enforced as a preflight secret requirement, mirroring
// registry.trustBundleRef, not here.
func validateEnvironmentEntitlementSatellite(owner string, sat *v1alpha1.EnvironmentEntitlementRHSMSatellite) []string {
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

func validateEnvironmentEntitlementRegistryCredentialsRequired(owner string, registry *v1alpha1.EnvironmentEntitlementRegistry) []string {
	if registry == nil {
		return []string{owner + ".credentialsRef is required"}
	}
	if registry.CredentialsRef.Name == "" {
		return []string{owner + ".credentialsRef is required"}
	}
	return nil
}

func validateEnvironmentEntitlementRegistry(owner string, registry *v1alpha1.EnvironmentEntitlementRegistry) []string {
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
