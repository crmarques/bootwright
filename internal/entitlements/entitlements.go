package entitlements

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

type RHSM struct {
	Management        string
	OrganizationPath  string
	ActivationKeyPath string
	ConnectToInsights bool
	Satellite         RHSMSatellite
}

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

func Resolve(ents []v1alpha1.Entitlement, idx secret.Index, name, defaultRegistryURL, secretsDir string) (Resolved, bool) {
	entitlement, ok := v1alpha1.EntitlementByName(ents, name)
	if !ok {
		return Resolved{}, false
	}
	provider, product, _ := v1alpha1.EntitlementTypeProviderProduct(entitlement.Spec.Type)
	out := Resolved{
		Name:     entitlement.Metadata.Name,
		Provider: provider,
		Product:  product,
	}
	if rhsm := v1alpha1.EntitlementEffectiveRHSM(ents, name); rhsm != nil {
		out.RHSM.Management = v1alpha1.EntitlementRHSMManagement(rhsm)
		if out.RHSM.Management != v1alpha1.EntitlementRHSMManagementExternal {
			out.RHSM.OrganizationPath = secret.ResolveMaterialPath(rhsm.OrganizationRef.Name, idx, secretsDir, secret.MaterialPrimary)
			out.RHSM.ActivationKeyPath = secret.ResolveMaterialPath(rhsm.ActivationKeyRef.Name, idx, secretsDir, secret.MaterialPrimary)
			out.RHSM.ConnectToInsights = rhsm.ConnectToInsights
			if rhsm.Satellite != nil {
				out.RHSM.Satellite = RHSMSatellite{
					Hostname:        rhsm.Satellite.Hostname,
					ContentBaseURL:  rhsm.Satellite.ContentBaseURL,
					TrustBundlePath: secret.ResolveMaterialPath(rhsm.Satellite.TrustBundleRef.Name, idx, secretsDir, secret.MaterialPrimary),
				}
			}
		}
	}
	if entitlement.Spec.Registry != nil {
		out.Registry = Registry{
			URL:             entitlement.Spec.Registry.URL,
			CredentialsPath: secret.ResolveMaterialPath(entitlement.Spec.Registry.CredentialsRef.Name, idx, secretsDir, secret.MaterialPrimary),
			TrustBundlePath: secret.ResolveMaterialPath(entitlement.Spec.Registry.TrustBundleRef.Name, idx, secretsDir, secret.MaterialPrimary),
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
