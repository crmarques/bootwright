package locality

import (
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/state/view"
)

type Policy struct {
	Deps Deps
}

type Deps struct {
	Hostname       func() (string, error)
	InterfaceAddrs func() ([]net.Addr, error)
	LookupIP       func(string) ([]net.IP, error)
}

type Result struct {
	OK       bool
	Evidence string
}

var DefaultPolicy = Policy{}

func DefaultDeps() Deps {
	return Deps{
		Hostname:       os.Hostname,
		InterfaceAddrs: net.InterfaceAddrs,
		LookupIP:       net.LookupIP,
	}
}

func CheckController(_ v1alpha1.State, _ Policy) Result {
	return Result{OK: true, Evidence: "bastion actions run on localhost"}
}

func IsControllerLocalMachine(machine v1alpha1.Machine, policy Policy) bool {
	if v1alpha1.MachineDeclaresLocalAccess(machine) {
		return true
	}
	if machine.Spec.Access.SSH == nil {
		return false
	}
	return IsControllerLocalAddress(v1alpha1.MachineSSHAddress(machine), policy)
}

func IsControllerLocalAddress(address string, policy Policy) bool {
	host := addressHost(address)
	if host == "" {
		return false
	}
	if stateview.IsLoopbackAlias(host) {
		return true
	}
	deps := policy.deps()
	if hostname, err := deps.Hostname(); err == nil && hostMatches(host, hostname) {
		return true
	}
	addrs, err := deps.InterfaceAddrs()
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return addressMatchesLocalIP(ip, addrs)
	}
	if deps.LookupIP == nil {
		return false
	}
	ips, err := deps.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if addressMatchesLocalIP(ip, addrs) {
			return true
		}
	}
	return false
}

func (p Policy) deps() Deps {
	deps := p.Deps
	defaults := DefaultDeps()
	if deps.Hostname == nil {
		deps.Hostname = defaults.Hostname
	}
	if deps.InterfaceAddrs == nil {
		deps.InterfaceAddrs = defaults.InterfaceAddrs
	}
	return deps
}

func addressHost(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
		return strings.TrimSpace(u.Hostname())
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(address, "[]")
}

func hostMatches(a, b string) bool {
	a = normalizedHostName(a)
	b = normalizedHostName(b)
	if a == "" || b == "" {
		return false
	}
	return a == b || shortHostName(a) == shortHostName(b)
}

func normalizedHostName(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func shortHostName(host string) string {
	if before, _, ok := strings.Cut(host, "."); ok {
		return before
	}
	return host
}

func localIP(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func addressMatchesLocalIP(ip net.IP, addrs []net.Addr) bool {
	for _, addr := range addrs {
		if local := localIP(addr); local != nil && local.Equal(ip) {
			return true
		}
	}
	return false
}
