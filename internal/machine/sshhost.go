package machine

import (
	"net/netip"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/trust"
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

func SSHReservationQualifier(address string, port int) string {
	return "ssh/" + sshAddress(address) + ":" + strconv.Itoa(port) + "/"
}

// SSHHostKeyQualifierBytes is the length of every host-key qualifier: the
// SHA-256 digest is 43 characters of unpadded base64.
const SSHHostKeyQualifierBytes = len("sshkey/SHA256:") + 43 + len("/")

// SSHHostKeyQualifier names an SSH host by the key its declared known-hosts
// entry pins, so two addresses of one host qualify claims alike (D188). The
// fingerprint is OpenSSH's SHA256 form in URL-safe base64, which holds no
// slash to end the qualifier early.
func SSHHostKeyQualifier(knownHosts, address string, port int) (string, error) {
	key, err := trust.ParseKnownHostsLine(knownHosts, address, port)
	if err != nil {
		return "", err
	}
	qualifier, ok := HostKeyQualifier(key)
	if !ok {
		return "", placementFailure("trust.identity", "the known-hosts entry carries no SSH host key whose fingerprint can be read", "")
	}
	return qualifier, nil
}

func HostKeyQualifier(key trust.HostKey) (string, bool) {
	fingerprint := key.Fingerprint()
	if len(fingerprint) != SSHHostKeyQualifierBytes-len("sshkey/")-len("/") {
		return "", false
	}
	return "sshkey/" + strings.NewReplacer("+", "-", "/", "_").Replace(fingerprint) + "/", true
}

func DeclaredAddresses(controller api.Object) []string {
	var addresses []string
	for _, declared := range controller.Spec().Get("network", "addresses").Items() {
		if value, _, _ := strings.Cut(declared.Get("address").Text(), "/"); value != "" {
			addresses = append(addresses, value)
		}
	}
	return addresses
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
	for _, value := range DeclaredAddresses(controller) {
		if sshAddress(value) == host {
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
