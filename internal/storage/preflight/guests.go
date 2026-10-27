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

type GuestChecks struct{ Commands Commands }
type guestProbe struct {
	id          string
	argv        []string
	accepts     func(string) bool
	requirement string
}

func (g GuestChecks) CheckGuest(ctx context.Context, contextName string, host api.Object, request PreflightRequest) ([]Check, error) {
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
		{"host.runtime", []string{"/usr/bin/podman", "--version"}, nonempty, "the native container runtime must answer its version query"},
		{"host.time", []string{"/usr/bin/timedatectl", "show", "--property=NTPSynchronized", "--value"}, func(text string) bool { return strings.TrimSpace(text) == "yes" }, "host time synchronization must be active"},
		{"host.privilege", []string{"/usr/bin/id", "-u"}, func(text string) bool { return strings.TrimSpace(text) == "0" }, "noninteractive root privilege must be available"},
		{"host.dns", []string{"/usr/bin/getent", "ahosts", hostname}, nonempty, "the declared host identity must resolve through the host's resolver"},
	}
	ssh, _ := machine.SSH(host)
	checks := make([]Check, 0, len(probes))
	for _, probe := range probes {
		data, err := g.Commands.Read(ctx, CommandRequest{ContextName: contextName, Machine: host.Name(), Address: ssh.Address, Port: ssh.Port, ObserveUntrusted: request.TrustOnFirstUse, SSH: request.SSH, Argv: probe.argv})
		var untrusted UntrustedKey
		if errors.As(err, &untrusted) {
			clear(data)
			reason := "the SSH host key is not enrolled"
			if untrusted.Fingerprint != "" {
				reason += "; observed " + untrusted.Fingerprint
			}
			return []Check{{ID: "host.trust", Target: host.Name(), Status: capacity.Unknown, Reason: reason, Next: "run bootwright machine trust --context " + contextName + " --machines " + host.Name()}}, nil
		}
		if probe.id == "host.privilege" && err == nil && strings.TrimSpace(string(data)) != "0" {
			clear(data)
			data, err = g.Commands.Read(ctx, CommandRequest{ContextName: contextName, Machine: host.Name(), SSH: request.SSH, Argv: []string{"/usr/bin/sudo", "-n", "/usr/bin/id", "-u"}})
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		check := Check{ID: probe.id, Target: host.Name(), Status: capacity.Unknown, Reason: "the bounded prerequisite read did not complete", Next: probe.requirement}
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
	return checks, nil
}

func UntrustedIdentity(err error) bool {
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Code == "trust.identity" {
			return true
		}
	}
	return false
}
