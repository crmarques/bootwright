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
	if entitlement.RHSM != nil {
		out.RHSM = RHSM{
			OrganizationPath:  secret.ResolveMaterialPath(entitlement.RHSM.OrganizationRef.Name, env, secretsDir, secret.MaterialPrimary),
			ActivationKeyPath: secret.ResolveMaterialPath(entitlement.RHSM.ActivationKeyRef.Name, env, secretsDir, secret.MaterialPrimary),
			ConnectToInsights: entitlement.RHSM.ConnectToInsights,
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
