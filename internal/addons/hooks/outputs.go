package hooks

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

// OutputPath returns where a captured hook output persists. Secret outputs land
// under clusters/<cluster>/secrets/addons/<addon>/hooks/<hook>/<name> (0600,
// reclaimed after the manifests apply), non-secret outputs under
// clusters/<cluster>/runtime/addons/<addon>/hooks/<hook>/<name>. The layout
// mirrors the Data Foundation attachment-details records.
func OutputPath(clustersDir, cluster, addon, hook string, output v1alpha1.ClusterAddonHookOutput) string {
	area := "runtime"
	if output.Secret {
		area = "secrets"
	}
	return filepath.Join(clustersDir, cluster, area, "addons", addon, "hooks", hook, output.Name)
}
