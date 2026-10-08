package installation

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/substrate"
)

// Implementation is the frozen identity of this capability. A plan records it,
// and a continuation refuses when the executable no longer offers it.
const Implementation = "os-install-anaconda-v1"

// Kind is the API kind this capability realizes. The substrate realizes the
// same kind through another implementation, so a block resolves by both.
const Kind = "Machine"

const requestVersion = "os-install-anaconda-v7"

// installationBudgets are the waits every installation this build plans
// freezes. The installer takes minutes to write a disk; a physical server adds
// its own power-on self test before it answers; and its fleet account answers
// once sshd has started, after the identity channel already does. Each is
// generous and bounded rather than open-ended.
var installationBudgets = Budgets{
	Installer:    Budget{Attempts: 180, DelaySeconds: 20},
	Identity:     Budget{Attempts: 120, DelaySeconds: 30},
	Reachability: Budget{Attempts: 30, DelaySeconds: 10},
}

// mediaMargin is what a run's deadline allows beyond its budgets: the bound of
// every call an apply makes to the machine's controller, and an hour for
// everything else, publishing the package tree, building the installer image
// and each read's own time between the budgets' pauses. A recorded lab-rhel
// installation took 1h10m7s, a flat hour of it the identity poll, so
// everything else, its controller calls and the installer's own wait
// included, took about ten minutes
// (.agents/knowledge/installation-completion-proof.md); an hour is six times
// that.
const mediaMargin = time.Hour + controllerCalls

// controllerCalls are the calls an apply makes to the machine's controller,
// each allowed its bound: the pre-boot power read, the insert, the power-off,
// the boot selection and the power-on that boot the installer, the eject and
// the disk selection and power-on that boot the installed system, and the
// eject the verification repeats. That is the libvirt arm's sequence. The
// bare-metal arm powers nothing off and inspects its machine where the libvirt
// arm reads its power, so its sequence stays within this one while that
// inspection makes at most eight requests: the system, its interface
// collection and six interfaces.
const controllerCalls = substrate.ControllerPowerReadBound + substrate.ControllerInsertBound +
	3*substrate.ControllerPowerBound + 2*substrate.ControllerBootSelectionBound + 2*substrate.ControllerEjectBound

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

// frozenTooling is the tooling closure an earlier build named here, which the
// controller stage now owns. It stays in the content digest unchanged, so
// deleting the name moved no frozen plan.
const frozenTooling = "lorax,xorriso"

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
		frozenTooling,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}
