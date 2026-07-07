package entitlements

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

type RHSM struct {
	OrganizationPath  string
	ActivationKeyPath string
	ConnectToInsights bool
	Satellite         RHSMSatellite
}

// RHSMSatellite is the resolved form of a corporate Red Hat Satellite the RHSM
// registration is redirected to. Hostname is empty when registration uses the
// public Red Hat CDN. TrustBundlePath is the materialized Satellite CA on the
// controller (basenamed by the install marker so it stays stable across runs).
type RHSMSatellite struct {
	Hostname        string
	ContentBaseURL  string
	TrustBundlePath string
}

type Registry struct {
	URL             string
	CredentialsPath string
	TrustBundlePath string
}

type License struct {
	Accepted bool
}

type Resolved struct {
	Name     string
	Provider string
	Product  string
	RHSM     RHSM
	Registry Registry
	License  License
}

// Find returns the Entitlement named name from the loaded set, if present.
func Find(ents []v1alpha1.Entitlement, name string) (v1alpha1.Entitlement, bool) {
	if name == "" {
		return v1alpha1.Entitlement{}, false
	}
	for _, entitlement := range ents {
		if entitlement.Metadata.Name == name {
			return entitlement, true
		}
	}
	return v1alpha1.Entitlement{}, false
}

// Resolve materializes the Entitlement named name into subscription, registry
// and license facts with secret material resolved to on-disk paths. ents is the
// lookup and rhelEntitlementRef-follow domain; env is needed only to resolve
// secret material (secrets live on Environment.spec.secrets), keeping the
// secret-by-name invariant. provider/product are derived from spec.type so the
// day-2 cephadm render is unchanged.
func Resolve(ents []v1alpha1.Entitlement, env *v1alpha1.Environment, name, defaultRegistryURL, secretsDir string) (Resolved, bool) {
	entitlement, ok := Find(ents, name)
	if !ok {
		return Resolved{}, false
	}
	provider, product, _ := v1alpha1.EntitlementTypeProviderProduct(entitlement.Spec.Type)
	out := Resolved{
		Name:     entitlement.Metadata.Name,
		Provider: provider,
		Product:  product,
	}
	// An entitlement either carries rhsm inline (redhat-rhel, redhat-ceph) or,
	// for ibm-storage-ceph, defers it to a referenced redhat-rhel entitlement.
	// Either way the resolved RHSM is populated identically, so downstream
	// rendering does not distinguish the two.
	rhsm := entitlement.Spec.RHSM
	if rhsm == nil && entitlement.Spec.RHELEntitlementRef.Name != "" {
		if rhel, ok := Find(ents, entitlement.Spec.RHELEntitlementRef.Name); ok {
			rhsm = rhel.Spec.RHSM
		}
	}
	if rhsm != nil {
		out.RHSM = RHSM{
			OrganizationPath:  secret.ResolveMaterialPath(rhsm.OrganizationRef.Name, env, secretsDir, secret.MaterialPrimary),
			ActivationKeyPath: secret.ResolveMaterialPath(rhsm.ActivationKeyRef.Name, env, secretsDir, secret.MaterialPrimary),
			ConnectToInsights: rhsm.ConnectToInsights,
		}
		if rhsm.Satellite != nil {
			out.RHSM.Satellite = RHSMSatellite{
				Hostname:        rhsm.Satellite.Hostname,
				ContentBaseURL:  rhsm.Satellite.ContentBaseURL,
				TrustBundlePath: secret.ResolveMaterialPath(rhsm.Satellite.TrustBundleRef.Name, env, secretsDir, secret.MaterialPrimary),
			}
		}
	}
	if entitlement.Spec.Registry != nil {
		out.Registry = Registry{
			URL:             entitlement.Spec.Registry.URL,
			CredentialsPath: secret.ResolveMaterialPath(entitlement.Spec.Registry.CredentialsRef.Name, env, secretsDir, secret.MaterialPrimary),
			TrustBundlePath: secret.ResolveMaterialPath(entitlement.Spec.Registry.TrustBundleRef.Name, env, secretsDir, secret.MaterialPrimary),
		}
	}
	if out.Registry.URL == "" {
		out.Registry.URL = defaultRegistryURL
	}
	if entitlement.Spec.License != nil {
		out.License.Accepted = entitlement.Spec.License.Accept
	}
	return out, true
}
