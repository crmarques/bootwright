package machine

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

type Connection string

const (
	ConnectionLocal     Connection = "local"
	ConnectionSSH       Connection = "ssh"
	ConnectionInstalled Connection = "installed"
)

const InstallFamily = "os-install"

// Placement fixes where one block's effect runs. The local arm needs no
// address or credential; an SSH placement fixes the login and elevation.
type Placement struct {
	Address       string     `json:"address,omitempty"`
	Connection    Connection `json:"connection"`
	KnownHostsRef string     `json:"knownHostsRef,omitempty"`
	Machine       string     `json:"machine"`
	Port          int        `json:"port,omitempty"`
	PrivateKeyRef string     `json:"privateKeyRef,omitempty"`
	Privilege     string     `json:"privilege,omitempty"`
	User          string     `json:"user,omitempty"`
}

func (p Placement) Local() bool { return p.Connection == ConnectionLocal }

// Installed reports the arm that reaches a Machine this context installs: over
// SSH as the account its installation created, which escalates through the
// passwordless sudo rule that installation wrote, against the host key its
// installation proved, so the placement binds no host-key Secret.
func (p Placement) Installed() bool { return p.Connection == ConnectionInstalled }

func (p Placement) Sudo() bool { return p.Privilege == "sudo" }

func (p Placement) PinnedSSH() bool {
	return p.Connection == ConnectionSSH && sshPrivilege(p.User, p.Privilege) && api.ValidLexical(api.LexicalHost, p.Address) &&
		p.Port >= 1 && p.Port <= 65535 && api.ValidLexical(api.LexicalName, p.PrivateKeyRef) && api.ValidLexical(api.LexicalName, p.KnownHostsRef)
}

func sshPrivilege(user, privilege string) bool {
	if user == "root" {
		return privilege == "" || privilege == "direct"
	}
	return privilege == "sudo" && api.ValidLexical(api.LexicalPOSIXUser, user)
}

func LifecyclePlacement(o api.Object, c api.Catalog) (Placement, bool) {
	if _, managed := BootstrapOf(o); !managed && !placementHost(o, c) {
		return Placement{}, false
	}
	controller := controllerForPlacement(c)
	if providerHost(o, c) {
		placement, err := PlacementFor(o, controller)
		return placement, err == nil
	}
	placement, err := ServicePlacementFor(o, controller)
	return placement, err == nil
}

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
// local. A provided host may dedicate an identity to lifecycle work.
func PlacementFor(machine api.Object, controllerMachine string) (Placement, error) {
	if machine.Name() == controllerMachine {
		return Placement{Connection: ConnectionLocal, Machine: machine.Name()}, nil
	}
	ssh := machine.Spec().Get("access", "ssh")
	if !ssh.Present() {
		return Placement{}, machinePlacementNoAccess.Fail("a placement host must be the controller or declare SSH access", "place the work on the controller Machine, or author access.ssh on "+machine.Identity())
	}
	if login := machine.Spec().Get("access", "lifecycle"); login.Present() {
		return dedicatedPlacement(machine, ssh, login)
	}
	if ssh.Has("auth", "operatorIdentity") {
		return Placement{}, machinePlacementAuthUnsupported.Fail("operator SSH identity is unsupported for lifecycle placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if ssh.Has("auth", "passwordRef") {
		return Placement{}, machinePlacementAuthUnsupported.Fail("password SSH authentication is unsupported for lifecycle placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("auth", "privateKeyRef") {
		return Placement{}, machinePlacementKeyMissing.Fail("lifecycle placement requires an SSH private key reference", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("knownHostsRef") {
		return Placement{}, machinePlacementHostKeyMissing.Fail("lifecycle placement requires a bound SSH host key", "author access.ssh.knownHostsRef on "+machine.Identity())
	}
	if len(privilegeFields(ssh)) != 0 {
		return Placement{}, machinePlacementEscalates.Fail("a lifecycle placement host connects as root and never escalates", "set access.ssh.user to root or omit it, and remove access.ssh.sudoPasswordRef, on "+machine.Identity())
	}
	address, err := infrastructureservices.ResolveAddress(machine, ssh.Get("addressRef").Text())
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

func dedicatedPlacement(machine api.Object, ssh, login api.Value) (Placement, error) {
	user, privilege := login.Get("user").Text(), login.Get("privilege").Text()
	if privilege == "" || !sshPrivilege(user, privilege) {
		return Placement{}, machinePlacementEscalates.Fail("the lifecycle login must use root with direct privileges or a non-root account with passwordless sudo",
			"set spec.access.lifecycle.user and spec.access.lifecycle.privilege on "+machine.Identity())
	}
	address, err := infrastructureservices.ResolveAddress(machine, ssh.Get("addressRef").Text())
	if err != nil {
		return Placement{}, err
	}
	port := 22
	if value, ok := ssh.Get("port").Int64(); ok && value > 0 {
		port = int(value)
	}
	key, host := login.Get("privateKeyRef").Text(), login.Get("knownHostsRef").Text()
	if key == "" || host == "" {
		return Placement{}, machinePlacementKeyMissing.Fail("the lifecycle login requires a private key and a host pin", "set spec.access.lifecycle.privateKeyRef and spec.access.lifecycle.knownHostsRef on "+machine.Identity())
	}
	return Placement{Address: address, Connection: ConnectionSSH, KnownHostsRef: host,
		Machine: machine.Name(), Port: port, PrivateKeyRef: key, User: user, Privilege: privilege}, nil
}

func ServicePlacementFor(machine api.Object, controllerMachine string) (Placement, error) {
	if machine.Name() == controllerMachine || !installed(machine) {
		return PlacementFor(machine, controllerMachine)
	}
	ssh, found := SSH(machine)
	install, declared := infrastructureservices.AddressOf(machine, machine.Spec().Get("network", "installAddressRef").Text())
	if !found || !declared || ssh.Address != install || ssh.Port != 22 || ssh.PrivateKeyRef == "" || ssh.User == "" {
		return Placement{}, machineInstalledAccessMismatch.Fail("a service on a Machine Bootwright installs is reached at its install address on port 22 with the fleet key, as its installation configured it",
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
	if login := o.Spec().Get("access", "lifecycle"); login.Present() {
		bootstrap, enroll := BootstrapOf(o)
		if !api.MachineProvided(o) || !ssh.Present() || (!enroll && !placementHost(o, c)) || o.Name() == controllerForPlacement(c) {
			return []api.Issue{invariant("$.spec.access.lifecycle", "a dedicated lifecycle login requires a remote provided placement host",
				"select a provided placement host reached over SSH or remove spec.access.lifecycle")}
		}
		if enroll && (!bootstrap.Valid(login.Get("user").Text()) || login.Get("privilege").Text() != "sudo" ||
			login.Get("knownHostsRef").Text() != ssh.Get("knownHostsRef").Text() || o.Spec().Get("access", "rootLogin").Text() == "revoke") {
			return []api.Issue{invariant("$.spec.access.lifecycle.bootstrap", "enrollment requires a separate bootstrap identity, a shared explicit host pin and a non-root lifecycle login with sudo",
				"declare bootstrap access and keep the original administrator available")}
		}
		if _, err := dedicatedPlacement(o, ssh, login); err != nil {
			return []api.Issue{invariant("$.spec.access.lifecycle", err.Error(), "declare a key and host pin, and choose root with direct or a non-root user with sudo")}
		}
		return nil
	}
	if !ssh.Present() || !placementHost(o, c) || installed(o) && !providerHost(o, c) {
		return nil
	}
	var issues []api.Issue
	for _, field := range privilegeFields(ssh) {
		issues = append(issues, invariant("$.spec.access.ssh."+field, privilegeRefusals[field], privilegeRemediations[field]))
	}
	return issues
}

func controllerForPlacement(c api.Catalog) string {
	if environments := c.OfKind(api.Environment); len(environments) == 1 {
		return environments[0].Spec().Get("controller", "machineRef").Text()
	}
	return ""
}

// placementHost reports whether a managed capability places lifecycle effects on the Machine.
func placementHost(o api.Object, c api.Catalog) bool {
	if o.Spec().Get("firewall", "management").Text() == "managed" || ProvidedUpdateRelease(o) != "" {
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

func placementFailure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
