package networking

import (
	"crypto/sha256"
	"encoding/hex"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	Kind            = "OpenShiftClusterNetwork"
	Implementation  = "cluster-network-v1"
	requestVersion  = "cluster-network-request-v2"
	evidenceVersion = "cluster-network-evidence-v3"
)

func BlockID(cluster string) string { return reconciliation.BlockIdentity("cluster-network-", cluster) }
func ContentDigest() string {
	sum := sha256.Sum256([]byte(Implementation + "\x00" + requestVersion + "\x00" + evidenceVersion + "\x00" + co.Version))
	return hex.EncodeToString(sum[:])
}
