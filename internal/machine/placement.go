package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	ConnectionLocal = "local"
	ConnectionSSH   = "ssh"
)

// Placement fixes where one block's effect runs. The local arm needs no
// address or credential; the SSH arm names exactly one target and account.
type Placement struct {
	Address         string `json:"address,omitempty"`
	Connection      string `json:"connection"`
	KnownHostsRef   string `json:"knownHostsRef,omitempty"`
	Machine         string `json:"machine"`
	Port            int    `json:"port,omitempty"`
	PrivateKeyRef   string `json:"privateKeyRef,omitempty"`
	SudoPasswordRef string `json:"sudoPasswordRef,omitempty"`
	User            string `json:"user,omitempty"`
}

func (p Placement) Local() bool { return p.Connection == ConnectionLocal }

// SecretReferences names every declaration a placement needs bound before the
// operation registers.
func (p Placement) SecretReferences() []string {
	var references []string
	for _, reference := range []string{p.PrivateKeyRef, p.KnownHostsRef, p.SudoPasswordRef} {
		if reference != "" {
			references = append(references, reference)
		}
	}
	return references
}

// PlacementFor selects the arm one block runs through. The controller is
// local; every other host must author the SSH access the operation binds.
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
		Machine: machine.Name(), Port: port, PrivateKeyRef: ssh.Get("auth", "privateKeyRef").Text(),
		SudoPasswordRef: ssh.Get("sudoPasswordRef").Text(), User: user,
	}, nil
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
