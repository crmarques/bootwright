package installer

import (
	"github.com/crmarques/bootwright/api/v1alpha1"
	secret "github.com/crmarques/bootwright/internal/secrets"
	stateview "github.com/crmarques/bootwright/internal/state/view"
)

// vSphereCredentialRefNames lists, in binding order without duplicates, the
// vCenter credentialsRef names every vSphere provider the cluster's machines
// bind declares. It mirrors loadVSphereCredentials' traversal but reads no
// material, so the portable render can mint a {{ secret <name> }} token per
// vCenter without a secrets directory.
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

// loadVSphereCredentials resolves the vCenter user/password material for
// every vSphere provider the cluster's machines bind, so the real
// install-config carries credentials instead of secret-ref placeholders. It
// shares vSphereCredentialRefNames' traversal so the real and portable renders
// never diverge on which vCenters get credentials.
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
