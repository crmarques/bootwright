package steps

import (
	"path/filepath"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func OutputPath(clustersDir, cluster, addon, step string, output v1alpha1.ClusterAddonStepOutput) string {
	area := "runtime"
	if output.Secret {
		area = "secrets"
	}
	return filepath.Join(clustersDir, cluster, area, "addons", addon, "steps", step, output.Name)
}
