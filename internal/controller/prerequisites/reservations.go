package prerequisites

import (
	"net/netip"
	"strconv"
	"strings"
)

// HeldClaim is the claim another context holds that a wanted one conflicts
// with: that context, the class of the key, which is the key's leading word
// and never the path, name or socket it carries, and what ends the conflict.
type HeldClaim struct {
	Context, Class, Remediation string
}

// ConflictingContext names the first claim in held whose exclusive key
// conflicts with an exclusive claim in wanted. Two keys conflict
// when they are equal. Socket keys are the one class compared further: a
// wildcard bind address holds its port on every address, so a socket key at a
// wildcard address conflicts with every socket key at that port. A shared
// claim conflicts with nothing.
func ConflictingContext(held, wanted []HostReservation) (HeldClaim, bool) {
	keys := map[string]bool{}
	ports, wildcards := map[uint16]bool{}, map[uint16]bool{}
	for _, reservation := range wanted {
		if reservation.Shared {
			continue
		}
		for _, key := range reservation.Keys {
			keys[key] = true
			if socket, ok := socketKey(key); ok {
				ports[socket.port] = true
				wildcards[socket.port] = wildcards[socket.port] || socket.wildcard
			}
		}
	}
	for _, reservation := range held {
		if reservation.Shared {
			continue
		}
		for _, key := range reservation.Keys {
			if keys[key] {
				return heldClaim(reservation.Context, key), true
			}
			if socket, ok := socketKey(key); ok && (wildcards[socket.port] || socket.wildcard && ports[socket.port]) {
				return heldClaim(reservation.Context, key), true
			}
		}
	}
	return HeldClaim{}, false
}

// heldClaim names the conflict one held key makes. Its remedy is that
// context's removal or continuation, or, for a key this context's own
// declaration chooses, another choice of it.
func heldClaim(context, key string) HeldClaim {
	class, _, _ := strings.Cut(key, ":")
	remediation := "destroy or continue context " + context + " first"
	switch class {
	case "socket":
		remediation += ", or give this context's service another bind address or port"
	case "bridge":
		remediation += ", or give this context's managed libvirt attachment another bridge"
	}
	return HeldClaim{Context: context, Class: class, Remediation: remediation}
}

// SocketConflict is two of one context's own exclusive claims whose sockets
// could never both listen, with the socket each claims as address and port.
type SocketConflict struct {
	First, Second             HostReservation
	FirstSocket, SecondSocket string
}

// ConflictingSockets finds the first two of one context's own exclusive claims
// whose sockets conflict under the rule ConflictingContext applies between
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
