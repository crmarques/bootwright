package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

func vSphereCredentialRefNames(state v1alpha1.State, ci v1alpha1.ClusterInstall) []string {
	var names []string
	seen := map[string]bool{}
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
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

func loadVSphereCredentials(state v1alpha1.State, ci v1alpha1.ClusterInstall, resolver secret.Resolver) (map[string]InstallerUserPass, error) {
	out := map[string]InstallerUserPass{}
	for _, name := range vSphereCredentialRefNames(state, ci) {
		creds, err := readUserPassMaterial(resolver, name, secret.MaterialPrimary, "vCenter credentials")
		if err != nil {
			return nil, err
		}
		out[name] = creds
	}
	return out, nil
}
