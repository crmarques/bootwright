package managedservice

import "slices"

// SocketKeys are the listening sockets one service claims. A wildcard bind
// claims every endpoint address at that port as well, because the socket it
// opens conflicts with each of them. A key covers the port on every transport,
// so a UDP and a TCP listener on one port are the same exclusive claim.
func SocketKeys(bindAddress string, port int, endpoints []Endpoint) []string {
	formatted := FormatPort(port)
	keys := []string{"socket:" + bindAddress + ":" + formatted}
	if bindAddress == "0.0.0.0" || bindAddress == "::" {
		for _, endpoint := range endpoints {
			keys = append(keys, "socket:"+endpoint.Address+":"+formatted)
		}
	}
	return keys
}

// ReservationKeys are the exclusive host resources a service claims before its
// first effect, so a second context refuses rather than taking them.
func ReservationKeys(unit, contentRoot string, sockets []string) []string {
	keys := append([]string{"unit:" + unit, "path:" + contentRoot}, sockets...)
	slices.Sort(keys)
	return slices.Compact(keys)
}
