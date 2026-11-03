package preflight

import (
	"context"
	"errors"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/substrate/capacity"
)

const sudoCmd = "/usr/bin/sudo"

type GuestChecks struct{ Commands Commands }
type guestProbe struct {
	id          string
	argv        []string
	accepts     func(string) bool
	requirement string
}

func (g GuestChecks) CheckGuest(ctx context.Context, contextName string, host api.Object, requirements HostRequirements, request PreflightRequest) ([]Check, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g.Commands == nil {
		return []Check{{ID: "host.prerequisites", Target: host.Name(), Status: capacity.Unknown, Reason: "verified host command access is unavailable"}}, nil
	}
	hostname := host.Name()
	for _, address := range host.Spec().Get("network", "addresses").Items() {
		if address.Get("name").Text() == "fqdn" {
			hostname = address.Get("address").Text()
		}
	}
	nonempty := func(text string) bool { return strings.TrimSpace(text) != "" }
	probes := []guestProbe{
		{"host.identity", []string{"/usr/bin/hostname"}, func(text string) bool { return strings.TrimSpace(text) == hostname }, "the installed hostname must equal the declared Machine hostname"},
		{"host.kernel", []string{"/usr/bin/uname", "-r"}, nonempty, "the installed kernel must answer its release query"},
		{"host.os", []string{"/usr/bin/cat", "/etc/os-release"}, func(text string) bool { return strings.Contains(text, "ID=") }, "the installed OS must expose its identity"},
		{"host.time", []string{"/usr/bin/timedatectl", "show", "--property=NTPSynchronized", "--value"}, func(text string) bool { return strings.TrimSpace(text) == "yes" }, "host time synchronization must be active"},
		{"host.privilege", []string{"/usr/bin/id", "-u"}, func(text string) bool { return strings.TrimSpace(text) == "0" }, "noninteractive root privilege must be available"},
		{"host.dns", []string{"/usr/bin/getent", "ahosts", hostname}, nonempty, "the declared host identity must resolve through the host's resolver"},
	}
	ssh, _ := machine.SSH(host)
	if requirements.Fleet != nil {
		if check, refused := fleetPrerequisite(host, ssh, *requirements.Fleet); refused {
			return []Check{check}, nil
		}
	}
	read := func(argv []string) ([]byte, error) {
		return g.Commands.Read(ctx, CommandRequest{ContextName: contextName, Machine: host.Name(), Address: ssh.Address, Port: ssh.Port, ObserveUntrusted: request.TrustOnFirstUse, SSH: request.SSH, Argv: argv, Fleet: requirements.Fleet})
	}
	checks := make([]Check, 0, len(probes)+1)
	elevate := false
	for _, probe := range probes {
		data, err := read(probe.argv)
		var untrusted UntrustedKey
		if errors.As(err, &untrusted) {
			clear(data)
			reason, next := "the SSH host key is not enrolled", "run bootwright machine trust --context "+contextName+" --name "+host.Name()
			if ssh.KnownHostsRef != "" {
				reason = "the SSH host key Secret " + ssh.KnownHostsRef + " does not pin one host key for " + machine.SSHHost(ssh.Address, ssh.Port)
				next = "author Secret " + ssh.KnownHostsRef + " as the one known-hosts entry Machine/" + host.Name() + " offers at " + machine.SSHHost(ssh.Address, ssh.Port)
			}
			if untrusted.Fingerprint != "" {
				reason += "; observed " + untrusted.Fingerprint
			}
			return []Check{{ID: "host.trust", Target: host.Name(), Status: capacity.Unknown, Reason: reason, Next: next}}, nil
		}
		if requirements.Fleet != nil && errors.Is(err, ErrUnreached) {
			clear(data)
			return []Check{fleetUnreached(host, ssh, *requirements.Fleet)}, nil
		}
		if probe.id == "host.privilege" && err == nil && strings.TrimSpace(string(data)) != "0" {
			clear(data)
			data, err = read(append([]string{sudoCmd, "-n"}, probe.argv...))
			elevate = err == nil && probe.accepts(string(data))
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		check := Check{ID: probe.id, Target: host.Name(), Status: capacity.Unknown, Reason: "the bounded prerequisite read did not complete", Next: probe.requirement}
		var exit ExitStatus
		if errors.As(err, &exit) {
			check.Status = capacity.Failed
			check.Reason = probe.requirement
		}
		if err == nil {
			check.Status = capacity.Failed
			check.Reason = probe.requirement
			if probe.accepts(string(data)) {
				check.Status = capacity.Passed
				check.Reason = "the host proved this prerequisite"
				check.Next = ""
			}
		}
		clear(data)
		checks = append(checks, check)
	}
	if requirements.Peers != nil {
		check := peerNamesCheck(read, host.Name(), *requirements.Peers)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		checks = append(checks, check)
	}
	if ports := requirements.Ports; len(ports) != 0 {
		firewallRead := read
		if elevate {
			firewallRead = func(argv []string) ([]byte, error) {
				if len(argv) != 0 && argv[0] == firewallCmd {
					argv = append([]string{sudoCmd, "-n"}, argv...)
				}
				return read(argv)
			}
		}
		check := firewallCheck(ctx, firewallRead, host.Name(), ports)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		checks = append(checks, check)
	}
	return checks, nil
}

func peerNamesCheck(read func([]string) ([]byte, error), name string, peers PeerNames) Check {
	check := Check{ID: "host.peer-names", Target: name, Status: capacity.Unknown, Reason: "the bounded peer name read did not complete",
		Next: "make " + name + "'s resolver answer every Ceph node name"}
	if len(peers.Names) == 0 {
		check.Status, check.Reason, check.Next = capacity.Passed, "the host proved this prerequisite", ""
		return check
	}
	resolves := func(names []string) (bool, bool) {
		data, err := read(append([]string{"/usr/bin/getent", "ahosts"}, names...))
		defer clear(data)
		var exit ExitStatus
		switch {
		case err == nil:
			return strings.TrimSpace(string(data)) != "", true
		case errors.As(err, &exit):
			return false, true
		}
		return false, false
	}
	all, known := resolves(peers.Names)
	switch {
	case !known:
		return check
	case all:
		check.Status, check.Reason, check.Next = capacity.Passed, "the host proved this prerequisite", ""
		return check
	}
	unresolved := []string{}
	for _, peer := range peers.Names {
		found, known := resolves([]string{peer})
		if !known {
			return check
		}
		if !found {
			unresolved = append(unresolved, peer)
		}
	}
	if len(unresolved) == 0 {
		return check
	}
	check.Status = capacity.Failed
	check.Reason = "Machine/" + name + " runs " + strings.Join(peers.Services, ", ") + " and does not resolve " + strings.Join(unresolved, ", ") + ", which cephadm writes into its configuration by name"
	return check
}

func fleetPrerequisite(host api.Object, ssh machine.SSHTarget, fleet FleetAccess) (Check, bool) {
	check := Check{ID: "host.access", Target: host.Name(), Status: capacity.Unknown}
	switch {
	case ssh.KnownHostsRef == "":
		check.Reason = "the provided tiebreaker Machine/" + host.Name() + " pins no SSH host key, and the fleet key is never offered to an unpinned host"
		check.Next = "author access.ssh.knownHostsRef on Machine/" + host.Name() + " pinning its host key"
	case fleet.KeyRef == "":
		check.Reason = "the provided tiebreaker Machine/" + host.Name() + " is dialed with the fleet SSH key, and the Environment declares none"
		check.Next = "set Environment spec.remoteMachinesAccessKey.keyRef to an sshKeyPair Secret and authorize its public half for user " + fleet.User + " on Machine/" + host.Name()
	default:
		return Check{}, false
	}
	return check, true
}

func fleetUnreached(host api.Object, ssh machine.SSHTarget, fleet FleetAccess) Check {
	return Check{ID: "host.access", Target: host.Name(), Status: capacity.Unknown,
		Reason: "the fleet connection to the provided tiebreaker Machine/" + host.Name() + " at " + machine.SSHHost(ssh.Address, ssh.Port) + " was not established",
		Next: "authorize the public half of Secret " + fleet.KeyRef + " (bootwright secret show --name " + fleet.KeyRef + " --part public-key) for user " + fleet.User + " on Machine/" + host.Name() +
			", and check that it offers the host key Secret " + ssh.KnownHostsRef + " pins"}
}

func UntrustedIdentity(err error) bool {
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Code == "trust.identity" {
			return true
		}
	}
	return false
}
