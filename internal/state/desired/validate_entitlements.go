package desiredstate

import (
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/entitlements"
)

// validateEntitlements enforces the per-type arm matrix on every Entitlement.
// spec.type is the discriminator; the required arms follow from it. Name
// uniqueness across kinds is handled by duplicateNameFindings; this validator
// surfaces an invalid metadata.name and the type/arm rules.
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

// rejectRHELEntitlementRef refuses a rhelEntitlementRef on any type but
// ibm-storage-ceph — only IBM Storage Ceph borrows a separate RHEL subscription.
func rejectRHELEntitlementRef(owner string, spec v1alpha1.EntitlementSpec) []string {
	if spec.RHELEntitlementRef.Name != "" {
		return []string{owner + ".rhelEntitlementRef is only valid for the ibm-storage-ceph type"}
	}
	return nil
}

// validateEntitlementRHELRef checks that an ibm-storage-ceph entitlement's
// rhelEntitlementRef names an existing redhat-rhel entitlement — the RHEL
// subscription IBM Storage Ceph runs on but does not itself carry.
func validateEntitlementRHELRef(owner string, ref v1alpha1.LocalObjectReference, state v1alpha1.State) []string {
	if ref.Name == "" {
		return []string{owner + " is required for ibm-storage-ceph; name a redhat-rhel entitlement for the RHEL subscription"}
	}
	target, ok := entitlements.Find(state.Entitlements, ref.Name)
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
	var errs []string
	if rhsm.OrganizationRef.Name == "" {
		errs = append(errs, owner+".organizationRef is required")
	}
	if rhsm.ActivationKeyRef.Name == "" {
		errs = append(errs, owner+".activationKeyRef is required")
	}
	return append(errs, validateEntitlementSatellite(owner+".satellite", rhsm.Satellite)...)
}

// validateEntitlementSatellite checks an optional corporate Satellite redirect on
// an rhsm arm: a bare hostname is required when the block is present, and
// contentBaseURL (when set) must be an http(s) URL. The CA secret named by
// trustBundleRef is enforced as a preflight secret requirement, mirroring
// registry.trustBundleRef, not here.
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
