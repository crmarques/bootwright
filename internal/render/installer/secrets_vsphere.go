package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// loadVSphereCredentials resolves the vCenter user/password material for
// every vSphere provider the cluster's machines bind, so the real
// install-config carries credentials instead of secret-ref placeholders.
func loadVSphereCredentials(state v1alpha1.State, ci v1alpha1.ClusterInstall, resolver secret.Resolver) (map[string]InstallerUserPass, error) {
	out := map[string]InstallerUserPass{}
	for _, machine := range ci.Machines {
		if machine.Source.ProfileRef.Name == "" {
			continue
		}
		provider, ok := stateview.Provider(state, machine.Source.ProviderRef.Name)
		if !ok || provider.Spec.Type != v1alpha1.ProvisionerVSphere || provider.Spec.VSphere == nil {
			continue
		}
		for _, vc := range provider.Spec.VSphere.VCenters {
			name := vc.CredentialsRef.Name
			if name == "" {
				continue
			}
			if _, seen := out[name]; seen {
				continue
			}
			creds, err := readUserPassMaterial(resolver, name, secret.MaterialPrimary, "vCenter credentials")
			if err != nil {
				return nil, err
			}
			out[name] = InstallerUserPass(creds)
		}
	}
	return out, nil
}
