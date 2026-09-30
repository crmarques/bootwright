package prerequisites

import (
	"net/netip"
	"strconv"
	"strings"
)

// ConflictingContext names the first context in held whose exclusive claim
// conflicts with an exclusive claim in wanted. Two keys conflict when they are
// equal. Socket keys are the one class compared further: a wildcard bind
// address holds its port on every address, so a socket key at a wildcard
// address conflicts with every socket key at that port. A shared claim
// conflicts with nothing.
func ConflictingContext(held, wanted []HostReservation) (string, bool) {
	keys := map[string]bool{}
	ports, wildcards := map[uint16]bool{}, map[uint16]bool{}
	for _, reservation := range wanted {
		if reservation.Shared {
			continue
		}
		for _, key := range reservation.Keys {
			keys[key] = true
			if wildcard, port, ok := socketKey(key); ok {
				ports[port] = true
				wildcards[port] = wildcards[port] || wildcard
			}
		}
	}
	for _, reservation := range held {
		if reservation.Shared {
			continue
		}
		for _, key := range reservation.Keys {
			if keys[key] {
				return reservation.Context, true
			}
			if wildcard, port, ok := socketKey(key); ok && (wildcards[port] || wildcard && ports[port]) {
				return reservation.Context, true
			}
		}
	}
	return "", false
}

// socketKey reads a `socket:<address>:<port>` key. The port follows the last
// colon, so an unbracketed IPv6 address still reads whole.
func socketKey(key string) (bool, uint16, bool) {
	rest, found := strings.CutPrefix(key, "socket:")
	separator := strings.LastIndexByte(rest, ':')
	if !found || separator < 0 {
		return false, 0, false
	}
	port, err := strconv.ParseUint(rest[separator+1:], 10, 16)
	if err != nil {
		return false, 0, false
	}
	address, err := netip.ParseAddr(rest[:separator])
	if err != nil {
		return false, uint16(port), true
	}
	return address.Unmap().IsUnspecified(), uint16(port), true
}
