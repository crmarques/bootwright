package machine

import (
	"net/netip"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// SSHHost names the host an SSH endpoint reaches as a known_hosts token names
// it, by address and port, so two Machines declaring one SSH address and port
// are one host whatever they are called, and one address at two ports, as a
// forwarded port gives, is two. An IP reads in its canonical form and a DNS
// name without case or a final dot, because planning resolves no name.
func SSHHost(address string, port int) string {
	host := sshAddress(address)
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(port)
}

// ReachesController reports an SSH endpoint that is the controller itself:
// port 22 at a loopback address, at localhost or at an address the controller
// Machine declares. Another port at one of them may be forwarded to another
// host, so it names a host of its own.
func ReachesController(controller api.Object, address string, port int) bool {
	if port != 22 {
		return false
	}
	host := sshAddress(address)
	if parsed, err := netip.ParseAddr(host); host == "localhost" || err == nil && parsed.IsLoopback() {
		return true
	}
	for _, declared := range controller.Spec().Get("network", "addresses").Items() {
		value, _, _ := strings.Cut(declared.Get("address").Text(), "/")
		if value != "" && sshAddress(value) == host {
			return true
		}
	}
	return false
}

func sshAddress(address string) string {
	if parsed, err := netip.ParseAddr(address); err == nil {
		return parsed.Unmap().String()
	}
	return strings.TrimSuffix(strings.ToLower(address), ".")
}
