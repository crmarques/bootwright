package infrastructureservices

import (
	"fmt"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// MaxReservationSockets is the most sockets a managed service claims: beside
// its unit and its content root, 62 sockets keep its host reservation within
// the 64 keys one reservation holds.
const MaxReservationSockets = 62

// ReservationSockets is the number of distinct sockets a managed service's
// claim spells on its placement Machine, each an address at a port.
func ReservationSockets(service, machine api.Object) int {
	spec := service.Spec()
	bind := spec.Get("bindAddress").Text()
	sockets := map[string]bool{}
	claim := func(address, port string) { sockets[address+" "+port] = true }
	switch service.Kind() {
	case api.Proxy, api.DNSServer, api.NTPServer:
		port := spec.Get("port").Text()
		claim(bind, port)
		if wildcard(bind) {
			for _, endpoint := range spec.Get("endpoints").Items() {
				if address := machineAddress(machine, endpoint.Get("addressRef").Text()); address != "" {
					claim(address, port)
				}
			}
		}
	case api.ArtifactServer:
		ports := map[string]string{}
		for _, listener := range spec.Get("listeners").Items() {
			ports[listener.Get("name").Text()] = listener.Get("port").Text()
			claim(bind, listener.Get("port").Text())
		}
		if wildcard(bind) {
			for _, endpoint := range spec.Get("endpoints").Items() {
				port, named := ports[endpoint.Get("listenerRef").Text()]
				if address := machineAddress(machine, endpoint.Get("addressRef").Text()); named && address != "" {
					claim(address, port)
				}
			}
		}
	default:
		return 0
	}
	return len(sockets)
}

// validateReservationBound refuses a managed service whose claim exceeds the
// keys one controller host reservation holds, which the store would refuse
// later without naming the object.
func validateReservationBound(o api.Object, machine api.Object) []api.Issue {
	count := ReservationSockets(o, machine)
	if count <= MaxReservationSockets {
		return nil
	}
	bind := o.Spec().Get("bindAddress").Text()
	if o.Kind() != api.ArtifactServer {
		return []api.Issue{{
			Code: "api.value", Field: "$.spec.endpoints",
			Message:     fmt.Sprintf("a managed %s bound to the wildcard %s claims one host reservation key for each distinct endpoint address beside its unit, its content root and its bind socket, within the 64 keys one reservation holds, so it declares at most %d distinct endpoint addresses; this one declares %d", o.Kind(), bind, MaxReservationSockets-1, count-1),
			Remediation: fmt.Sprintf("declare at most %d distinct endpoint addresses in spec.endpoints of %s", MaxReservationSockets-1, o.Identity()),
		}}
	}
	field := "$.spec.listeners"
	if wildcard(bind) {
		field = "$.spec.endpoints"
	}
	return []api.Issue{{
		Code: "api.value", Field: field,
		Message:     fmt.Sprintf("a managed ArtifactServer claims one host reservation key for each listener and, under a wildcard bindAddress, for each distinct endpoint address of each listener, beside its unit and its content root, within the 64 keys one reservation holds, so its spec.listeners and spec.endpoints claim at most %d sockets; this one claims %d", MaxReservationSockets, count),
		Remediation: fmt.Sprintf("declare fewer listeners in spec.listeners or fewer endpoints with distinct addresses in spec.endpoints of %s, so they claim at most %d sockets", o.Identity(), MaxReservationSockets),
	}}
}
