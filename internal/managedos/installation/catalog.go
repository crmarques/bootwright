package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Implementation is the frozen identity of this capability. A plan records it,
// and a continuation refuses when the executable no longer offers it.
const Implementation = "os-install-anaconda-v1"

// Kind is the API kind this capability realizes. The substrate realizes the
// same kind through another implementation, so a block resolves by both.
const Kind = "Machine"

const requestVersion = "os-install-anaconda-v3"

// priorRequestVersion is the version this capability reads but no longer
// writes, so a context applied under it is removable by this build. It froze a
// libvirt guest as a domain and a hypervisor URI, before the target its
// substrate derives replaced both.
const priorRequestVersion = "os-install-anaconda-v2"

// consumerPrefix is the subtree this capability owns beneath a managed artifact
// server's served root. The server owns the root; this block owns exactly
// os/<object>/ and removes it in its own inverse.
const consumerPrefix = "os"

// servedRoot is the directory the managed artifact server serves, beneath its
// own content root.
const servedRoot = "public"

// privatePrefix is the subtree material only the installing machine may read
// is published under. This block owns private/<object>/; the attempt mints the
// unguessable segment beneath it, so no path here is ever guessable from the
// plan, the evidence or any log.
const privatePrefix = "private"

// IdentityFile is the name the delivered host key pair is published under,
// inside the attempt's own unguessable directory.
const IdentityFile = "identity"

// MarkerPath is where a completed installation leaves its proof, and
// HostKeyPath is where it republishes the guest's own SSH host public key.
// Both are read through the substrate's identity operation, never over the
// network. The key is republished here rather than read where sshd keeps it
// because a confined guest agent cannot read `sshd_key_t`, and widening that
// policy would let the channel reach the private halves too.
const (
	MarkerPath  = "/etc/bootwright/install-marker.json"
	HostKeyPath = "/etc/bootwright/host-key.pub"
)

// installTooling is the closure that builds a per-Machine installer image and
// extracts a DVD tree. On the controller the controller stage installs it; on
// an SSH host this block does.
var installTooling = []string{"lorax", "xorriso"}

// InstallTooling is that closure in canonical order.
func InstallTooling() []string { return append([]string(nil), installTooling...) }

// BlockID names the block this capability contributes. A consumer states
// requirements by API object, never by this identity.
func BlockID(machine string) string { return "os-install-" + machine }

// ContentDigest binds a plan to the exact behavior this build implements, so
// changing the request shape, the derived Kickstart or the published layout
// invalidates a frozen plan.
func ContentDigest() string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"bootwright.managedos.install-anaconda-v1",
		Implementation, requestVersion, kickstartVersion,
		consumerPrefix, privatePrefix, servedRoot, MarkerPath, HostKeyPath,
		strings.Join(installTooling, ","),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
