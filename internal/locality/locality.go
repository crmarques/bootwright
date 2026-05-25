package locality

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type Policy struct {
	RequireBastion bool
	Deps           Deps
}

type Deps struct {
	InterfaceAddrs func() ([]net.Addr, error)
	LookupIP       func(string) ([]net.IP, error)
	Hostname       func() (string, error)
}

type Result struct {
	OK       bool
	Evidence string
}

var DefaultPolicy = Policy{RequireBastion: true, Deps: DefaultDeps()}

func DefaultDeps() Deps {
	return Deps{
		InterfaceAddrs: net.InterfaceAddrs,
		LookupIP:       net.LookupIP,
		Hostname:       os.Hostname,
	}
}

func CheckBastion(state v1alpha1.State, policy Policy) Result {
	if !policy.RequireBastion {
		return Result{OK: true, Evidence: "bastion locality policy disabled"}
	}
	deps := policy.Deps
	if deps.InterfaceAddrs == nil {
		deps.InterfaceAddrs = net.InterfaceAddrs
	}
	if deps.LookupIP == nil {
		deps.LookupIP = net.LookupIP
	}
	if deps.Hostname == nil {
		deps.Hostname = os.Hostname
	}
	env := primaryEnvironment(state)
	if env == nil || env.Spec.Bastion == nil || env.Spec.Bastion.HostRef == "" {
		return Result{OK: false, Evidence: "Environment.spec.bastion.hostRef is required"}
	}
	host, ok := hostByName(state, env.Spec.Bastion.HostRef)
	if !ok {
		return Result{OK: false, Evidence: fmt.Sprintf("bastion Host %q not found", env.Spec.Bastion.HostRef)}
	}
	local, err := localAddresses(deps)
	if err != nil {
		return Result{OK: false, Evidence: err.Error()}
	}
	if ok, evidence := matchHost(host, local, deps.LookupIP); ok {
		return Result{OK: true, Evidence: evidence}
	}
	return Result{OK: false, Evidence: fmt.Sprintf("local host does not match bastion Host %q", host.Metadata.Name)}
}

func primaryEnvironment(state v1alpha1.State) *v1alpha1.Environment {
	if len(state.Environments) == 0 {
		return nil
	}
	return &state.Environments[0]
}

func hostByName(state v1alpha1.State, name string) (v1alpha1.Host, bool) {
	for _, host := range state.Hosts {
		if host.Metadata.Name == name {
			return host, true
		}
	}
	return v1alpha1.Host{}, false
}

type localSet struct {
	all  map[string]net.IP
	name map[string]bool
}

func localAddresses(deps Deps) (localSet, error) {
	out := localSet{all: map[string]net.IP{}, name: map[string]bool{}}
	add := func(ip net.IP) {
		if ip == nil {
			return
		}
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		out.all[ip.String()] = ip
	}
	addrs, err := deps.InterfaceAddrs()
	if err != nil {
		return out, fmt.Errorf("list local interface addresses: %w", err)
	}
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			add(v.IP)
		case *net.IPAddr:
			add(v.IP)
		}
	}
	if hostname, err := deps.Hostname(); err == nil && hostname != "" {
		out.name[strings.ToLower(hostname)] = true
		if ips, err := deps.LookupIP(hostname); err == nil {
			for _, ip := range ips {
				add(ip)
			}
		}
	}
	out.name["localhost"] = true
	return out, nil
}

func matchHost(host v1alpha1.Host, local localSet, lookupIP func(string) ([]net.IP, error)) (bool, string) {
	var matches []string
	hasNonLoopbackCandidate := false
	for _, address := range host.Spec.Addresses {
		value := strings.TrimSpace(address.Address)
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		if local.name[lower] {
			if lower == "localhost" {
				matches = append(matches, value)
				continue
			}
			return true, fmt.Sprintf("bastion address %s resolves to local hostname", value)
		}
		ips := resolveAddress(value, lookupIP)
		if len(ips) == 0 && net.ParseIP(value) == nil && lower != "localhost" {
			hasNonLoopbackCandidate = true
		}
		for _, ip := range ips {
			if !ip.IsLoopback() {
				hasNonLoopbackCandidate = true
			}
			if localIP, ok := local.all[ip.String()]; ok {
				if !localIP.IsLoopback() {
					return true, fmt.Sprintf("bastion address %s matches local IP %s", value, ip.String())
				}
				matches = append(matches, value)
			}
		}
	}
	if !hasNonLoopbackCandidate && len(matches) > 0 {
		return true, "bastion declares only loopback-local addresses"
	}
	return false, ""
}

func resolveAddress(value string, lookupIP func(string) ([]net.IP, error)) []net.IP {
	if ip := net.ParseIP(value); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		return []net.IP{ip}
	}
	ips, err := lookupIP(value)
	if err != nil {
		return nil
	}
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		out = append(out, ip)
	}
	return out
}
