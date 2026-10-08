package prerequisites

import (
	"net/netip"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// MaxReservationKeys is the most keys one host reservation holds, as the
// controller record stores them (specs/contexts/controller-record.md, Bounds).
const MaxReservationKeys = 64

// Conflict is one exclusive key another context holds and the key of this
// context's claim it conflicts with.
type Conflict struct {
	Held, Wanted       HostReservation
	HeldKey, WantedKey string
}

// Conflicts lists every exclusive key in held that conflicts with an exclusive
// key in wanted, in stored order, each paired with the first wanted key it
// conflicts with. Two keys conflict when they are equal; when both are socket
// keys at one port and either binds a wildcard address, which holds its port
// on every address; and when both are managed network prefixes that overlap,
// identical or nested, since the host routes each prefix to its own bridge. A
// shared claim conflicts with nothing.
func Conflicts(held, wanted []HostReservation) []Conflict {
	var found []Conflict
	for _, holder := range held {
		if holder.Shared {
			continue
		}
		for _, heldKey := range holder.Keys {
			if conflict, ok := firstConflict(holder, heldKey, wanted); ok {
				found = append(found, conflict)
			}
		}
	}
	return found
}

func firstConflict(holder HostReservation, heldKey string, wanted []HostReservation) (Conflict, bool) {
	for _, claim := range wanted {
		if claim.Shared {
			continue
		}
		for _, wantedKey := range claim.Keys {
			if keysConflict(heldKey, wantedKey) {
				return Conflict{Held: holder, Wanted: claim, HeldKey: heldKey, WantedKey: wantedKey}, true
			}
		}
	}
	return Conflict{}, false
}

func keysConflict(first, second string) bool {
	if first == second {
		return true
	}
	if ours, ok := socketKey(first); ok {
		theirs, ok := socketKey(second)
		return ok && ours.port == theirs.port && (ours.wildcard || theirs.wildcard)
	}
	ours, ok := prefixKey(first)
	if !ok {
		return false
	}
	theirs, ok := prefixKey(second)
	return ok && ours.Overlaps(theirs)
}

// prefixKey reads a `prefix:<masked prefix>` key.
func prefixKey(key string) (netip.Prefix, bool) {
	rest, found := strings.CutPrefix(key, "prefix:")
	if !found {
		return netip.Prefix{}, false
	}
	prefix, err := netip.ParsePrefix(rest)
	return prefix, err == nil
}

// conflictKinds are the API kinds of the claim kinds a diagnostic names as
// objects.
var conflictKinds = map[string]api.Kind{
	"substrate-host":     api.InfraProvider,
	"substrate-machine":  api.Machine,
	"substrate-physical": api.Machine,
	"os-install":         api.Machine,
	"proxy":              api.Proxy,
	"dns":                api.DNSServer,
	"ntp":                api.NTPServer,
	"artifact-server":    api.ArtifactServer,
	"cluster-media":      api.ContainerCluster,
}

// ConflictDiagnostic names the holding context, the key it holds, the holder's
// object and this context's claiming object. It prints a socket, a bridge or a
// prefix, and only the class of any other key, because a path, unit, libvirt
// or BMC key may carry a private value. Only a completed destroy releases a
// held key, so the remedy is that destroy or the field that chooses the key.
// It is a function rather than a method so Conflict stays a plain value.
func ConflictDiagnostic(c Conflict) diagnostics.Diagnostic {
	class, _, _ := strings.Cut(c.HeldKey, ":")
	wantedPart := "the same " + class + " reservation"
	switch class {
	case "socket", "bridge", "prefix":
		wantedPart = describeKey(c.WantedKey)
	}
	message := "context " + c.Held.Context + " holds " + describeKey(c.HeldKey) + " for " + conflictObject(c.Held) +
		", which conflicts with " + wantedPart + " that " + conflictObject(c.Wanted) + " of this context claims"
	remediation := "run bootwright destroy --context " + c.Held.Context + " first" + conflictField(c.Wanted, class)
	diagnostic := diagnostics.Diagnostic{Severity: "error", Code: "controller.conflict", Message: message, Remediation: remediation}
	if kind, ok := conflictKinds[c.Wanted.Kind]; ok {
		diagnostic.Object = &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(kind), Name: c.Wanted.Service}
	}
	return diagnostic
}

func conflictObject(claim HostReservation) string {
	if kind, ok := conflictKinds[claim.Kind]; ok {
		return string(kind) + "/" + claim.Service
	}
	return claim.Kind + " " + claim.Service
}

func describeKey(key string) string {
	class, value, _ := strings.Cut(key, ":")
	switch class {
	case "socket":
		if socket, ok := socketKey(key); ok {
			return "socket " + socket.text()
		}
	case "bridge":
		return "bridge " + value
	case "prefix":
		return "managed network prefix " + value
	}
	return "a " + class + " reservation"
}

// conflictField names the field that chooses a key of this context's claim,
// for the claims whose declaration chooses it.
func conflictField(wanted HostReservation, class string) string {
	object := conflictObject(wanted)
	switch {
	case class == "socket" && (wanted.Kind == "proxy" || wanted.Kind == "dns" || wanted.Kind == "ntp"):
		return ", or change spec.bindAddress or spec.port on " + object
	case class == "socket" && wanted.Kind == "artifact-server":
		return ", or change spec.bindAddress or a spec.listeners[].port on " + object
	case class == "socket" && wanted.Kind == "substrate-machine":
		return ", or change spec.libvirt.bmcEmulationDefaults.bindAddress or spec.libvirt.bmcEmulationDefaults.port on the InfraProvider of " + object
	case class == "bridge" && wanted.Kind == "substrate-host":
		return ", or change spec.networkAttachments[].libvirt.bridge on " + object
	case class == "bmc" && wanted.Kind == "substrate-physical":
		return ", or change spec.hardware.management.bmc.address on " + object
	case class == "prefix" && wanted.Kind == "substrate-host":
		return ", or change spec.networkAttachments[].libvirt.address on " + object
	}
	return ""
}

// SocketConflict is two of one context's own exclusive claims whose sockets
// could never both listen, with the socket each claims as address and port.
type SocketConflict struct {
	First, Second             HostReservation
	FirstSocket, SecondSocket string
}

// ConflictingSockets finds the first two of one context's own exclusive claims
// whose sockets conflict under the rule Conflicts applies between
// contexts. A claim never conflicts with itself, since a wildcard bind's
// endpoint keys are that one socket, and only sockets are compared, because
// one context's claims may share another key by design, such as the path of a
// package tree two installations of one profile publish together.
func ConflictingSockets(reservations []HostReservation) (SocketConflict, bool) {
	for later, second := range reservations {
		if second.Shared {
			continue
		}
		for _, first := range reservations[:later] {
			if first.Shared {
				continue
			}
			for _, wanted := range second.Keys {
				theirs, ok := socketKey(wanted)
				if !ok {
					continue
				}
				for _, held := range first.Keys {
					ours, ok := socketKey(held)
					if ok && ours.port == theirs.port && (held == wanted || ours.wildcard || theirs.wildcard) {
						return SocketConflict{First: first, Second: second, FirstSocket: ours.text(), SecondSocket: theirs.text()}, true
					}
				}
			}
		}
	}
	return SocketConflict{}, false
}

type socket struct {
	address  string
	port     uint16
	wildcard bool
}

func (s socket) text() string {
	port := strconv.FormatUint(uint64(s.port), 10)
	if strings.Contains(s.address, ":") {
		return "[" + s.address + "]:" + port
	}
	return s.address + ":" + port
}

// socketKey reads a `socket:<address>:<port>` key. The port follows the last
// colon, so an unbracketed IPv6 address still reads whole.
func socketKey(key string) (socket, bool) {
	rest, found := strings.CutPrefix(key, "socket:")
	separator := strings.LastIndexByte(rest, ':')
	if !found || separator < 0 {
		return socket{}, false
	}
	port, err := strconv.ParseUint(rest[separator+1:], 10, 16)
	if err != nil {
		return socket{}, false
	}
	read := socket{address: rest[:separator], port: uint16(port)}
	if address, err := netip.ParseAddr(read.address); err == nil {
		read.wildcard = address.Unmap().IsUnspecified()
	}
	return read, true
}
