package libvirt

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/crmarques/bootwright/internal/substrate"
)

// HostImplementation and MachineImplementation are the frozen identities of the
// two capabilities this package offers. A plan records one per block, and a
// continuation refuses when the executable no longer offers it.
const (
	HostImplementation    = "substrate-host-libvirt-v1"
	MachineImplementation = "machine-libvirt-v1"
)

// HostKind and MachineKind are the API kinds the two capabilities realize. Two
// implementations claim MachineKind, so a block resolves by both.
const (
	HostKind    = "InfraProvider"
	MachineKind = "Machine"
)

// emulatorImage is the management controller image, pinned by content digest.
// It was resolved from the publisher's latest tag and its runtime is recorded
// in .agents/knowledge/sushy-tools-emulated-bmc.md.
const emulatorImage = "quay.io/metal3-io/sushy-tools@sha256:f760343718e1175343f230ec43496a4f0d850f1d0a3139b92db1e475ac91d11e"

// hypervisorPackages is the closure a libvirt provider host runs. It is the
// same set the controller stage installs when the host is the controller.
var hypervisorPackages = []string{
	"libvirt-client", "libvirt-daemon", "libvirt-daemon-driver-network", "libvirt-daemon-driver-qemu",
	"libvirt-daemon-driver-storage-core", "qemu-img", "qemu-kvm", "swtpm", "swtpm-tools",
}

// HypervisorPackages is the closure a provider host needs, in canonical order.
func HypervisorPackages() []string { return append([]string(nil), hypervisorPackages...) }

// serviceUnit is the daemon whose socket the declared URI answers on.
const serviceUnit = "libvirtd.service"

const (
	hostRequestVersion    = "substrate-host-libvirt-v1"
	machineRequestVersion = "machine-libvirt-v1"
)

// HostBlockID and MachineBlockID name the blocks each capability contributes.
// A consumer states requirements by API object, never by these identities.
func HostBlockID(provider string) string { return "substrate-host-" + provider }

func MachineBlockID(machine string) string { return "machine-" + machine }

// HostContentDigest and MachineContentDigest bind a plan to the exact behavior
// this build implements, so changing the request shape, the pinned emulator or
// the host layout invalidates a frozen plan.
func HostContentDigest() string {
	return contentDigest("bootwright.substrate.libvirt.host-v1", HostImplementation, hostRequestVersion,
		strings.Join(hypervisorPackages, ","), substrate.ImagePrefix, substrate.Prefix, serviceUnit)
}

func MachineContentDigest() string {
	return contentDigest("bootwright.substrate.libvirt.machine-v1", MachineImplementation, machineRequestVersion,
		emulatorImage, substrate.ImagePrefix, substrate.Prefix)
}

func contentDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
