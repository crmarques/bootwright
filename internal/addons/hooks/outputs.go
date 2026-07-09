package hooks

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func OutputPath(clustersDir, cluster, addon, hook string, output v1alpha1.ClusterAddonHookOutput) string {
	area := "runtime"
	if output.Secret {
		area = "secrets"
	}
	return filepath.Join(clustersDir, cluster, area, "addons", addon, "hooks", hook, output.Name)
}
