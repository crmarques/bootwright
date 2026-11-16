package instance

import (
	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Kind            = "ClusterAddonBinding"
	Implementation  = "addon-instance-v1"
	requestVersion  = "addon-instance-request-v1"
	evidenceVersion = "addon-instance-evidence-v1"
)

func BlockID(pkg, cluster string) string {
	return reconciliation.BlockIdentity("addon-", pkg+"-"+cluster)
}
func Object(cluster, pkg string) string { return cluster + "." + pkg }
func ContentDigest() string {
	return reconciliation.ContentDigestOf(Implementation, requestVersion, evidenceVersion, clusterobjects.Version, clusterobjects.OwnerDomain, clusterobjects.ObjectDomain, clusterobjects.ExactRule, clusterobjects.OperatorManagedRule, clusterobjects.DependentExactRule)
}
