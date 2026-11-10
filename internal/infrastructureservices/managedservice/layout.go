package managedservice

import (
	"maps"
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ForeignListenerReason names the refusal an adapter reports when something
// other than its service already holds a socket the service binds.
const ForeignListenerReason = "foreign-listener"

// ForeignListenerRefusals is the diagnostic each port a service binds is
// refused with when the adapter finds a listener this host's reservations do
// not know of, keyed by the reason and port the adapter names: the service as
// the refused object, the socket it binds and what to change. A resolver and a
// time service keep the one port validation permits, so their remedy offers
// only another bind address.
func ForeignListenerRefusals(kind, service, bindAddress string, ports []int) map[string]error {
	identity := kind + "/" + service
	refusals := map[string]error{}
	for _, port := range ports {
		wildcard := bindAddress == "0.0.0.0" || bindAddress == "::"
		message := "something other than " + identity + " already listens at " + HostPort(bindAddress, port) + ", so its unit was not started"
		remedy := "stop what listens there, or choose another bindAddress on " + identity
		if kind != string(api.DNSServer) && kind != string(api.NTPServer) {
			remedy = "stop what listens there, or choose another bindAddress or port on " + identity
		}
		if wildcard {
			remedy += "; the retained run output lists every socket found"
			message = "something other than " + identity + " already listens on port " + FormatPort(port) +
				" at an address its wildcard bind " + bindAddress + " covers, so its unit was not started"
		}
		refusals[ForeignListenerReason+"-"+FormatPort(port)] = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state", Message: message,
			Remediation: remedy,
			Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: kind, Name: service},
		}}}
	}
	return refusals
}

const ContainerRuntimeReason = "container-runtime"

func ContainerRuntimeRefusals(kind, service, machine string, ports []int) map[string]error {
	identity := kind + "/" + service
	refusals := map[string]error{}
	for _, port := range ports {
		refusals[ContainerRuntimeReason+"-"+FormatPort(port)] = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity:    "error",
			Code:        "lifecycle.state",
			Message:     "Machine/" + machine + " has no working /usr/bin/podman, so " + identity + " cannot run its container; this attempt pulled, published or started nothing",
			Remediation: "install the podman package on Machine/" + machine + " with its package manager and repeat the apply, or place " + identity + " on a Machine that runs containers",
			Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: kind, Name: service},
		}}}
	}
	return refusals
}

func ListenerRefusals(kind, service, bindAddress string, ports []int) map[string]error {
	refusals := ForeignListenerRefusals(kind, service, bindAddress, ports)
	if bindAddress == "0.0.0.0" || bindAddress == "::" {
		return refusals
	}
	for _, port := range ports {
		refusals["unassigned-bind-address-"+FormatPort(port)] = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
			Severity: "error", Code: "lifecycle.state",
			Message:     "the placement host has no usable local bind address for " + HostPort(bindAddress, port) + "; this attempt pulled, published or started nothing",
			Remediation: "restore the declared address on the placement host, or destroy this context with its original build before importing an assigned bindAddress and matching endpoints and TLS certificate names on " + kind + "/" + service,
			Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: kind, Name: service},
		}}}
	}
	return refusals
}

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

// ReadinessRefusals preserves uncertainty only for a silent listener.
func ReadinessRefusals(kind, service, machine, bindAddress string, ports []int) map[string]error {
	refusals := ListenerRefusals(kind, service, bindAddress, ports)
	maps.Copy(refusals, ContainerRuntimeRefusals(kind, service, machine, ports))
	for _, port := range ports {
		reasons := []string{"readiness-timeout", "listener-failed"}
		if kind == string(api.ArtifactServer) {
			reasons = append(reasons, "certificate-mismatch", "readiness-unproved")
		}
		for _, reason := range reasons {
			code := "lifecycle.state"
			message := "the listener refused or returned an invalid readiness answer"
			if reason == "readiness-timeout" {
				code = "lifecycle.unknown"
				message = "the listener did not answer within its bounded readiness window"
			}
			if reason == "readiness-unproved" {
				code = "lifecycle.unknown"
				message = "the listener supplied no usable readiness evidence"
			}
			if reason == "certificate-mismatch" {
				message = "the listener presented a different certificate than the operation bound"
			}
			refusals[reason+"-"+FormatPort(port)] = &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{Severity: "error", Code: code, Message: message + " on " + HostPort(bindAddress, port), Remediation: "restore the declared service listener and retry the operation", Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: kind, Name: service}}}}
		}
	}
	return refusals
}
