package entitlements

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

type RHSM struct {
	OrganizationPath  string
	ActivationKeyPath string
	ConnectToInsights bool
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

func Find(env *v1alpha1.Environment, name string) (v1alpha1.EnvironmentEntitlement, bool) {
	if env == nil || name == "" {
		return v1alpha1.EnvironmentEntitlement{}, false
	}
	for _, entitlement := range env.Spec.Entitlements {
		if entitlement.Name == name {
			return entitlement, true
		}
	}
	return v1alpha1.EnvironmentEntitlement{}, false
}

func Resolve(env *v1alpha1.Environment, name, defaultRegistryURL, secretsDir string) (Resolved, bool) {
	entitlement, ok := Find(env, name)
	if !ok {
		return Resolved{}, false
	}
	out := Resolved{
		Name:     entitlement.Name,
		Provider: entitlement.Provider,
		Product:  entitlement.Product,
	}
	// An entitlement either carries rhsm inline (redhat/rhel, redhat/ceph) or,
	// for ibm/ibm-storage-ceph, defers it to a referenced redhat/rhel
	// entitlement. Either way the resolved RHSM is populated identically, so
	// downstream rendering does not distinguish the two.
	rhsm := entitlement.RHSM
	if rhsm == nil && entitlement.RHELEntitlementRef.Name != "" {
		if rhel, ok := Find(env, entitlement.RHELEntitlementRef.Name); ok {
			rhsm = rhel.RHSM
		}
	}
	if rhsm != nil {
		out.RHSM = RHSM{
			OrganizationPath:  secret.ResolveMaterialPath(rhsm.OrganizationRef.Name, env, secretsDir, secret.MaterialPrimary),
			ActivationKeyPath: secret.ResolveMaterialPath(rhsm.ActivationKeyRef.Name, env, secretsDir, secret.MaterialPrimary),
			ConnectToInsights: rhsm.ConnectToInsights,
		}
	}
	if entitlement.Registry != nil {
		out.Registry = Registry{
			URL:             entitlement.Registry.URL,
			CredentialsPath: secret.ResolveMaterialPath(entitlement.Registry.CredentialsRef.Name, env, secretsDir, secret.MaterialPrimary),
			TrustBundlePath: secret.ResolveMaterialPath(entitlement.Registry.TrustBundleRef.Name, env, secretsDir, secret.MaterialPrimary),
		}
	}
	if out.Registry.URL == "" {
		out.Registry.URL = defaultRegistryURL
	}
	if entitlement.License != nil {
		out.License.Accepted = entitlement.License.Accept
	}
	return out, true
}
