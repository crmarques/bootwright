package prerequisites

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// RequiresClusterFIPS reads cluster intent and makes no claim about Bootwright cryptography.
func RequiresClusterFIPS(catalog api.Catalog) bool {
	for _, cluster := range catalog.OfKind(api.ContainerCluster) {
		if cluster.Spec().Get("security", "fips", "enabled").Bool() {
			return true
		}
	}
	return false
}

// CheckClusterFIPS requires a positive native host precondition before effects.
func CheckClusterFIPS(ctx context.Context, inspector FIPSInspector, catalog api.Catalog) error {
	if !RequiresClusterFIPS(catalog) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if inspector != nil {
		enabled, err := inspector.FIPSMode(ctx)
		if err == nil && enabled {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return failure("controller.unsupported", "a selected FIPS cluster requires the controller kernel FIPS flag /proc/sys/crypto/fips_enabled to be 1", "use a FIPS-enabled controller, then repeat preflight controller and plan")
}
