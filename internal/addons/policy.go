package addons

// DefaultClusterAddonFieldManager is the field manager Bootwright passes to
// server-side apply for add-on resources.
const DefaultClusterAddonFieldManager = "bootwright"

// ClusterAddonPolicy is the fixed execution policy applied to every add-on:
// how rendered resources are handed to oc apply. Bootwright owns these
// values; authored YAML cannot set them.
type ClusterAddonPolicy struct {
	Prune           bool
	ServerSideApply *bool
	FieldManager    string
	ContinueOnError bool
}

func (p ClusterAddonPolicy) UseServerSideApply() bool {
	return p.ServerSideApply == nil || *p.ServerSideApply
}

// DefaultPolicy is the policy every add-on plan runs under: server-side
// apply with the Bootwright field manager, no pruning.
func DefaultPolicy() ClusterAddonPolicy {
	enabled := true
	return ClusterAddonPolicy{
		ServerSideApply: &enabled,
		FieldManager:    DefaultClusterAddonFieldManager,
	}
}
