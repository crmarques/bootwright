package substrate

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// Prefix namespaces every host object a realized substrate owns, so two
// contexts on one host never name the same network, domain, pool or unit.
const Prefix = "bootwright"

// BootRedfishVirtualMedia is the one boot path this contract performs on a
// physical machine: its installer is presented to the machine's own management
// controller as virtual media.
const BootRedfishVirtualMedia = "redfishVirtualMedia"

// ImagePrefix is libvirt's own image tree, so a disk this product creates
// carries the labels the hypervisor expects.
const ImagePrefix = "/var/lib/libvirt/images/bootwright"

// MaxDataDisks is how many data disks a libvirt domain presents after its root
// disk, as vdb through vdh.
const MaxDataDisks = 7

// ProviderNameLimit keeps a provider's host block, substrate-host-<name>,
// inside the 63-byte block identity a plan admits.
const ProviderNameLimit = 48

// MachineNameLimit keeps the longest block a Machine contributes,
// os-install-<name>, inside the 63-byte block identity a plan admits. It holds
// for every Machine, because a Machine may change lifecycle.
const MachineNameLimit = 52

func NetworkName(contextName, attachment string) string {
	return Prefix + "-" + contextName + "-" + attachment
}

func PoolName(contextName, provider string) string {
	return Prefix + "-" + contextName + "-" + provider + "-vmedia"
}

func PoolPath(contextName, provider string) string {
	return ImagePrefix + "/" + contextName + "/" + provider + "/vmedia"
}

func DomainName(contextName, machine string) string {
	return Prefix + "-" + contextName + "-" + machine
}

func DiskDirectory(contextName, machine string) string {
	return ImagePrefix + "/" + contextName + "/" + machine
}

// ControllerUnitName is the host unit one Machine's emulated management
// controller runs as.
func ControllerUnitName(contextName, machine string) string {
	return Prefix + "-" + contextName + "-bmc-" + machine
}

// DomainUUID derives a domain's identity from the context and Machine names, so
// one declaration always realizes the same virtual machine and a replay finds
// the system it created rather than a new one. The value is a version-8 UUID:
// its bits are derived, not random, which is what a deterministic identity
// needs.
func DomainUUID(contextName, machine string) string {
	sum := sha256.Sum256([]byte("bootwright.substrate.libvirt.domain-v1\x00" + contextName + "\x00" + machine))
	sum[6] = sum[6]&0x0f | 0x80
	sum[8] = sum[8]&0x3f | 0x80
	value := hex.EncodeToString(sum[:16])
	return strings.Join([]string{value[0:8], value[8:12], value[12:16], value[16:20], value[20:32]}, "-")
}

// InterfaceMAC derives one interface's hardware address from the context,
// Machine and interface names. The address keeps QEMU's own locally
// administered prefix, so it can never collide with a real adapter.
func InterfaceMAC(contextName, machine, iface string) string {
	sum := sha256.Sum256([]byte("bootwright.substrate.libvirt.interface-v1\x00" + contextName + "\x00" + machine + "\x00" + iface))
	value := hex.EncodeToString(sum[:3])
	return "52:54:00:" + value[0:2] + ":" + value[2:4] + ":" + value[4:6]
}

// ControllerEndpoint is the Redfish address a consumer reaches one Machine's
// management controller at. The emulator serves plain HTTP, so the scheme is
// fixed rather than derived from a TLS choice it does not make.
func ControllerEndpoint(address string, port int, uuid string) string {
	return "http://" + ControllerSocket(address, port) + "/redfish/v1/Systems/" + uuid
}

// ControllerSocket is the socket one emulated controller listens on, as its
// endpoint and the plan print it: an IPv6 address is bracketed, so the colon
// before the port is never read as part of the address. The reservation key
// keeps the unbracketed socket form every listener claims.
func ControllerSocket(address string, port int) string {
	return bracketed(address) + ":" + FormatPort(port)
}

// HostedMachines lists the Machines one provider realizes, in canonical name
// order. That order is what the emulated controller port allocation counts, so
// it is derived once and never re-derived per Machine.
func HostedMachines(catalog api.Catalog, provider string) []api.Object {
	var found []api.Object
	for _, machine := range catalog.OfKind(api.Machine) {
		if machine.Spec().Get("substrate", "providerRef").Text() != provider {
			continue
		}
		if provided := machine.Spec().Get("os", "provided"); provided.Type() == api.Boolean && provided.Bool() {
			continue
		}
		found = append(found, machine)
	}
	slices.SortFunc(found, func(x, y api.Object) int { return strings.Compare(x.Name(), y.Name()) })
	return found
}

// ControllerPort allocates one Machine's emulated controller port from its
// provider's base port and that Machine's position in the provider's own order,
// which is the allocation the API contract fixes.
func ControllerPort(catalog api.Catalog, provider api.Object, machine string) (int, bool) {
	base, ok := provider.Spec().Get("libvirt", "bmcEmulationDefaults", "port").Int64()
	if !ok || base < 1 {
		return 0, false
	}
	for ordinal, hosted := range HostedMachines(catalog, provider.Name()) {
		if hosted.Name() == machine {
			port := base + int64(ordinal)
			return int(port), port <= 65535
		}
	}
	return 0, false
}

// SafeSegment repeats the host-identifier grammar at every boundary where a
// declared name reaches a unit, path or libvirt object name.
func SafeSegment(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, c := range value {
		alphanumeric := c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !alphanumeric && !(c == '-' && index != 0 && index != len(value)-1) {
			return false
		}
	}
	return true
}

func FormatPort(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
