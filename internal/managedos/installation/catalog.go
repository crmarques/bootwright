package installation

import (
	"strconv"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/substrate"
)

// Implementation is frozen; continuation requires the executable to offer it.
const Implementation = "os-install-anaconda-v1"

// Kind is shared with the substrate; resolution also requires the implementation.
const Kind = "Machine"

const requestVersion = "os-install-anaconda-v1"

// installationBudgets are the waits every installation this build plans
// freezes. The installer takes minutes to write a disk; a physical server adds
// its own power-on self test before it answers; and its fleet account answers
// once sshd has started, after the identity channel already does. Each is
// generous and bounded rather than open-ended.
var installationBudgets = Budgets{
	Installer:        Budget{Attempts: 180, DelaySeconds: 20},
	Identity:         Budget{Attempts: 120, DelaySeconds: 30},
	Reachability:     Budget{Attempts: 30, DelaySeconds: 10},
	NetworkApply:     Budget{Attempts: 24, DelaySeconds: 5},
	NetworkReconnect: Budget{Attempts: 12, DelaySeconds: 5},
	Reboot:           Budget{Attempts: 60, DelaySeconds: 10},
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
// the boot selection and the power-on that boot the installer, the eject, the
// disk selection and power-on that boot the installed system, the eject the
// verification repeats, and the disk selection and power-on of the restart
// that proves its network. That is the libvirt arm's sequence. The bare-metal
// arm has its own inspection and media budgets, composed by machineMediaMargin
// below from the same substrate-owned port table.
const controllerCalls = substrate.LibvirtPreBootBound + substrate.LibvirtMediaInsertBound +
	substrate.LibvirtBootMediaBound + 2*substrate.LibvirtBootDiskBound + 2*substrate.LibvirtMediaEjectBound

func machineMediaMargin(arm string, firmware *Firmware) time.Duration {
	bounds, ok := substrate.MachineBoundsFor(arm)
	if !ok {
		return mediaMargin
	}
	margin := time.Hour + bounds.PreBoot + bounds.MediaInsert + bounds.BootMedia + 2*bounds.BootDisk + 2*bounds.MediaEject
	if firmware.enforced() {
		margin += bounds.Firmware
	}
	return margin
}

// consumerPrefix owns os/<object>/ beneath the artifact server's served root.
const consumerPrefix = "os"

// servedRoot is beneath the artifact server's content root.
const servedRoot = "public"

// privatePrefix is the subtree material only the installing machine may read
// is published under. This block owns private/<object>/; the attempt mints the
// unguessable segment beneath it, so no path here is ever guessable from the
// plan, the evidence or any log.
const privatePrefix = "private"

// IdentityFile names the delivered host key pair in the private attempt directory.
const IdentityFile = "identity"

// RecoveryFile names the adjacent recovery passphrase.
const RecoveryFile = "recovery"

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

const RootWWNPath = "/etc/bootwright/root-wwn"

const (
	NetworkDocumentPath = "/etc/bootwright/network.yml"
	NetworkMarkerPath   = "/etc/bootwright/network-marker.json"
)

const networkCheckpointSeconds = 300

const networkRollbackGrace = 30 * time.Second

const (
	registrationSession     = 630 * time.Second
	releasePinSession       = 120 * time.Second
	satelliteSession        = 60 * time.Second
	contentViewSession      = 60 * time.Second
	unregistrationSession   = 330 * time.Second
	registrationInspection  = 30 * time.Second
	updateSession           = 1800 * time.Second
	updateGuestRead         = 120 * time.Second
	updateCompletionSession = 120 * time.Second
	updateMarkerRead        = 60 * time.Second
)

const (
	firewallSession  = 300 * time.Second
	firewallDeadline = firewallSession + 5*time.Minute
)

const registrationBound = max(registrationSession+releasePinSession+satelliteSession+contentViewSession, unregistrationSession) + registrationInspection

const updateBound = updateSession + updateRebootBound + updateGuestRead + updateCompletionSession + updateMarkerRead

const updateRebootBound = 600 * time.Second

func machineBootDisk(arm string) time.Duration {
	if bounds, ok := substrate.MachineBoundsFor(arm); ok {
		return bounds.BootDisk
	}
	return substrate.LibvirtBootDiskBound
}

const provedDiskBoot = "proved-installation-disk-boot,replay-safe-unread-registration,bond-permanent-lower-links"

// BlockID names the block this capability contributes. A consumer states
// requirements by API object, never by this identity.
func BlockID(machine string) string { return reconciliation.BlockIdentity("os-install-", machine) }

// ContentDigest binds a plan to the exact behavior this build implements, so
// changing the request shape, the derived Kickstart or the published layout
// invalidates a frozen plan.
func ContentDigest() string {
	return reconciliation.ContentDigestOf(
		"bootwright.managedos.install-anaconda-v1",
		Implementation, requestVersion, kickstartVersion,
		consumerPrefix, privatePrefix, servedRoot, MarkerPath, HostKeyPath,
		NetworkDocumentPath, NetworkMarkerPath, strconv.Itoa(networkCheckpointSeconds), provedDiskBoot, substrate.RedfishMediaPolicy,
		"anaconda-home-partition-policy",
	)
}
