package networking

import (
	"crypto/sha256"
	"encoding/hex"

	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

const (
	Kind            = "ContainerClusterNetwork"
	Implementation  = "cluster-network-v1"
	requestVersion  = "cluster-network-request-v1"
	evidenceVersion = "cluster-network-evidence-v2"
)

func BlockID(cluster string) string { return "cluster-network-" + cluster }
func ContentDigest() string {
	sum := sha256.Sum256([]byte(Implementation + "\x00" + requestVersion + "\x00" + evidenceVersion + "\x00" + co.Version))
	return hex.EncodeToString(sum[:])
}
