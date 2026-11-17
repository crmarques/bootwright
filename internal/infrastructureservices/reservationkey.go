package infrastructureservices

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// Key is one host reservation key; a reservation stores its String form, and
// an empty Qualifier claims it on the controller.
type Key struct {
	Qualifier      Qualifier
	Class, Subject string
}

func (k Key) String() string { return k.Qualifier.String() + k.Class + ":" + k.Subject }

type Qualifier struct{ Form, Host string }

const (
	SSHAddressQualifier = "ssh"
	SSHHostKeyQualifier = "sshkey"
)

func (q Qualifier) String() string {
	if q == (Qualifier{}) {
		return ""
	}
	return q.Form + "/" + q.Host + "/"
}

// SSHHostKeyQualifierBytes is the length of every host-key qualifier: the
// SHA-256 digest is 43 characters of unpadded base64.
const SSHHostKeyQualifierBytes = len("sshkey/SHA256:") + 43 + len("/")

func ParseKey(text string) (Key, bool) {
	qualifier, rest := unqualified(text)
	class, subject, found := strings.Cut(rest, ":")
	key := Key{Class: class, Subject: subject}
	if form, host, ok := strings.Cut(strings.TrimSuffix(qualifier, "/"), "/"); ok {
		key.Qualifier = Qualifier{Form: form, Host: host}
	}
	return key, found
}

func QualifiedKeys(qualifier Qualifier, keys []string) []string {
	qualified := make([]string, 0, len(keys))
	for _, key := range keys {
		qualified = append(qualified, qualifier.String()+key)
	}
	return qualified
}

// ControllerReservationKey claims one physical machine by its controller, so
// two contexts never drive one server. The host and port come from the
// endpoint itself, which is already normalized.
func ControllerReservationKey(endpoint string) Key {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return Key{Class: "bmc", Subject: endpoint}
	}
	return Key{Class: "bmc", Subject: controllerAuthority(parsed) + strings.TrimPrefix(parsed.Path, "/redfish/v1/Systems")}
}

// ControllerMediaReservationKey serializes media and shared security settings
// across Systems on the same BMC origin, within and across contexts.
func ControllerMediaReservationKey(endpoint string) Key {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return Key{Class: "bmc-media", Subject: endpoint}
	}
	return Key{Class: "bmc-media", Subject: controllerAuthority(parsed)}
}

func controllerAuthority(parsed *url.URL) string {
	host := parsed.Host
	if parsed.Port() == "" {
		port := "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
		host = parsed.Hostname()
		if address, err := netip.ParseAddr(host); err == nil && address.Is6() {
			host = "[" + host + "]"
		}
		host += ":" + port
	}
	return host
}

func SystemReservationKey(uuid string) Key {
	return Key{Class: "bmc-system", Subject: strings.ToLower(strings.TrimSpace(uuid))}
}

func MACReservationKey(mac string) Key {
	address := strings.TrimSpace(mac)
	if parsed, err := net.ParseMAC(address); err == nil {
		address = parsed.String()
	}
	return Key{Class: "bmc-mac", Subject: strings.ToLower(address)}
}

// MediaReservationKey is the shared host claim a context holds while an
// operation of its plan needs one image to stay exactly as it froze it.
func MediaReservationKey(name string) Key { return Key{Class: "media", Subject: name} }

func SSHReservationQualifier(address string, port int) Qualifier {
	host := strings.TrimSuffix(strings.ToLower(address), ".")
	if parsed, err := netip.ParseAddr(address); err == nil {
		host = parsed.Unmap().String()
	}
	return Qualifier{Form: SSHAddressQualifier, Host: host + ":" + strconv.Itoa(port)}
}

// HostKeyQualifier names an SSH host by the OpenSSH SHA256 fingerprint of the
// key its declared known-hosts entry pins, so two addresses of one host
// qualify claims alike (D188). The fingerprint is held in URL-safe base64,
// which holds no slash to end the qualifier early.
func HostKeyQualifier(fingerprint string) (Qualifier, bool) {
	if len(fingerprint) != SSHHostKeyQualifierBytes-len("sshkey/")-len("/") {
		return Qualifier{}, false
	}
	return Qualifier{Form: SSHHostKeyQualifier, Host: strings.NewReplacer("+", "-", "/", "_").Replace(fingerprint)}, true
}
