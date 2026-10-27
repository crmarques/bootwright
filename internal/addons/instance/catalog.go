package instance

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Kind            = "ClusterAddonBinding"
	Implementation  = "addon-instance-v1"
	requestVersion  = "addon-instance-request-v2"
	evidenceVersion = "addon-instance-evidence-v3"
)

func BlockID(pkg, cluster string) string {
	return reconciliation.BlockIdentity("addon-", pkg+"-"+cluster)
}
func Object(cluster, pkg string) string { return cluster + "." + pkg }
func ContentDigest() string {
	sum := sha256.Sum256([]byte(strings.Join([]string{Implementation, requestVersion, evidenceVersion, clusterobjects.Version, clusterobjects.OwnerDomain, clusterobjects.ObjectDomain, clusterobjects.ExactRule, clusterobjects.OperatorManagedRule, clusterobjects.DependentExactRule}, "\x00")))
	return hex.EncodeToString(sum[:])
}
