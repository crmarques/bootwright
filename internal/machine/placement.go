package machine

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	ConnectionLocal     = "local"
	ConnectionSSH       = "ssh"
	ConnectionInstalled = "installed"
	InstallFamily       = "os-install"
)

// Placement fixes where one block's effect runs. The local arm needs no
// address or credential; the SSH arm names exactly one target and connects as
// root without escalation.
type Placement struct {
	Address       string `json:"address,omitempty"`
	Connection    string `json:"connection"`
	KnownHostsRef string `json:"knownHostsRef,omitempty"`
	Machine       string `json:"machine"`
	Port          int    `json:"port,omitempty"`
	PrivateKeyRef string `json:"privateKeyRef,omitempty"`
	User          string `json:"user,omitempty"`
}

func (p Placement) Local() bool { return p.Connection == ConnectionLocal }

// Installed reports the arm that reaches a Machine this context installs: over
// SSH as the account its installation created, which escalates through the
// passwordless sudo rule that installation wrote, against the host key its
// installation proved, so the placement binds no host-key Secret.
func (p Placement) Installed() bool { return p.Connection == ConnectionInstalled }

// SecretReferences names every declaration a placement needs bound before the
// operation registers. An escalation Secret is never one of them.
func (p Placement) SecretReferences() []string {
	var references []string
	for _, reference := range []string{p.PrivateKeyRef, p.KnownHostsRef} {
		if reference != "" {
			references = append(references, reference)
		}
	}
	return references
}

func (p Placement) SecretClasses() secrets.Classes {
	return secrets.Classes{}.Add(secrets.AccessCredential, p.PrivateKeyRef).Add(secrets.TrustAnchor, p.KnownHostsRef)
}

// PlacementFor selects the arm one block runs through. The controller is
// local; every other host must author the SSH access the operation binds, and
// that access connects as root and never escalates.
func PlacementFor(machine api.Object, controllerMachine string) (Placement, error) {
	if machine.Name() == controllerMachine {
		return Placement{Connection: ConnectionLocal, Machine: machine.Name()}, nil
	}
	ssh := machine.Spec().Get("access", "ssh")
	if !ssh.Present() {
		return Placement{}, placementFailure("lifecycle.state", "a placement host must be the controller or declare SSH access", "place the work on the controller Machine, or author access.ssh on "+machine.Identity())
	}
	if ssh.Has("auth", "operatorIdentity") {
		return Placement{}, placementFailure("lifecycle.state", "operator SSH identity is unsupported for lifecycle placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if ssh.Has("auth", "passwordRef") {
		return Placement{}, placementFailure("lifecycle.state", "password SSH authentication is unsupported for lifecycle placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("auth", "privateKeyRef") {
		return Placement{}, placementFailure("lifecycle.state", "lifecycle placement requires an SSH private key reference", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("knownHostsRef") {
		return Placement{}, placementFailure("lifecycle.state", "lifecycle placement requires a bound SSH host key", "author access.ssh.knownHostsRef on "+machine.Identity())
	}
	if len(privilegeFields(ssh)) != 0 {
		return Placement{}, placementFailure("lifecycle.state", "a lifecycle placement host connects as root and never escalates", "set access.ssh.user to root or omit it, and remove access.ssh.sudoPasswordRef, on "+machine.Identity())
	}
	address, err := ResolveAddress(machine, ssh.Get("addressRef").Text())
	if err != nil {
		return Placement{}, err
	}
	port := 22
	if value, ok := ssh.Get("port").Int64(); ok && value > 0 {
		port = int(value)
	}
	user := ssh.Get("user").Text()
	if user == "" {
		user = "root"
	}
	return Placement{
		Address: address, Connection: ConnectionSSH, KnownHostsRef: ssh.Get("knownHostsRef").Text(),
		Machine: machine.Name(), Port: port, PrivateKeyRef: ssh.Get("auth", "privateKeyRef").Text(), User: user,
	}, nil
}

func ServicePlacementFor(machine api.Object, controllerMachine string) (Placement, error) {
	if machine.Name() == controllerMachine || !installed(machine) {
		return PlacementFor(machine, controllerMachine)
	}
	ssh, found := SSH(machine)
	install, declared := Address(machine, machine.Spec().Get("network", "installAddressRef").Text())
	if !found || !declared || ssh.Address != install || ssh.Port != 22 || ssh.PrivateKeyRef == "" || ssh.User == "" {
		return Placement{}, placementFailure("lifecycle.state", "a service on a Machine Bootwright installs is reached at its install address on port 22 with the fleet key, as its installation configured it",
			"remove spec.access.ssh from "+machine.Identity()+" so its access is the installation's own")
	}
	return Placement{Address: ssh.Address, Connection: ConnectionInstalled, Machine: machine.Name(), Port: ssh.Port, PrivateKeyRef: ssh.PrivateKeyRef, User: ssh.User}, nil
}

// privilegeFields names the access.ssh fields that keep a host from connecting
// as root without escalation: a user other than root, and an escalation Secret.
func privilegeFields(ssh api.Value) []string {
	var fields []string
	if ssh.Has("user") && ssh.Get("user").Text() != "root" {
		fields = append(fields, "user")
	}
	if ssh.Has("sudoPasswordRef") {
		fields = append(fields, "sudoPasswordRef")
	}
	return fields
}

var privilegeRefusals = map[string]string{
	"user":            "a lifecycle placement host connects as root; non-root SSH accounts are not supported",
	"sudoPasswordRef": "a lifecycle placement never escalates; remove access.ssh.sudoPasswordRef",
}

var privilegeRemediations = map[string]string{
	"user":            "remove spec.access.ssh.user, or set it to root",
	"sudoPasswordRef": "remove spec.access.ssh.sudoPasswordRef",
}

// validatePlacementHost refuses a lifecycle placement host whose SSH access
// would connect as another account or escalate. A Machine no placement names,
// such as a cluster node, a storage node or a session target, keeps any account.
func validatePlacementHost(o api.Object, c api.Catalog) []api.Issue {
	ssh := o.Spec().Get("access", "ssh")
	if !ssh.Present() || !placementHost(o, c) || installed(o) && !providerHost(o, c) {
		return nil
	}
	var issues []api.Issue
	for _, field := range privilegeFields(ssh) {
		issues = append(issues, invariant("$.spec.access.ssh."+field, privilegeRefusals[field], privilegeRemediations[field]))
	}
	return issues
}

// placementHost reports whether a managed infrastructure service or a libvirt
// provider places its lifecycle effects on the Machine.
func placementHost(o api.Object, c api.Catalog) bool {
	if o.Spec().Get("firewall", "management").Text() == "managed" {
		return true
	}
	for _, candidate := range c.Objects() {
		spec := candidate.Spec()
		if infrastructureservices.IsService(candidate.Kind()) && spec.Get("management").Text() == "managed" && spec.Get("machineRef").Text() == o.Name() {
			return true
		}
		if candidate.Kind() == api.InfraProvider && substrate.HostMachine(candidate) == o.Name() {
			return true
		}
	}
	return false
}

func providerHost(o api.Object, c api.Catalog) bool {
	return slices.ContainsFunc(c.OfKind(api.InfraProvider), func(provider api.Object) bool { return substrate.HostMachine(provider) == o.Name() })
}

// ResolveAddress answers the address one reference names on its Machine.
func ResolveAddress(machine api.Object, reference string) (string, error) {
	if reference == "" {
		return "", placementFailure("api.required", "the address reference is empty", "name an address on "+machine.Identity())
	}
	address, declared := Address(machine, reference)
	if !declared {
		return "", placementFailure("api.reference", "the address reference does not resolve on its Machine", "name an address declared on "+machine.Identity())
	}
	if address == "" {
		return "", placementFailure("api.value", "the referenced Machine address is empty", "correct the address on "+machine.Identity())
	}
	return address, nil
}

func placementFailure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
