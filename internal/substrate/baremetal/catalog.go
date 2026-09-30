package baremetal

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Implementation is the frozen identity of this capability. A plan records it,
// and a continuation refuses when the executable no longer offers it.
const Implementation = "machine-baremetal-v1"

// Kind is the API kind this capability realizes. Another implementation
// realizes the same kind for a machine its substrate creates, so a block
// resolves by kind and implementation together.
const Kind = "Machine"

const requestVersion = "machine-baremetal-v2"

// BlockID names the block this capability contributes. A Machine has exactly
// one provider, so the machine families of two substrates never collide.
func BlockID(machine string) string { return "machine-" + machine }

// ContentDigest binds a plan to the exact behavior this build implements. It
// covers the request shape and the proof this block performs, so widening what
// counts as proving a physical machine invalidates a frozen plan rather than
// silently applying to one.
func ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.substrate.baremetal.machine-v1", Implementation, requestVersion,
		"identity+addresses+power",
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
