package lifecycle

import (
	"context"
	"encoding/json"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
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

// MaterialFile names one operation-scoped file a bound Secret part is written
// to. Anything not listed never leaves bounded memory.
type MaterialFile struct {
	Name     string
	Part     secrets.Part
	Secret   string
	Variable string
}

// RunRequest is one authorized adapter invocation, in terms every capability
// shares. Implementation selects the automation, so two implementations of one
// kind never run each other's playbook. Material is bounded memory owned by the
// caller; the adapter writes it only to operation-scoped files it removes, and
// never to arguments, environment, evidence or logs.
type RunRequest struct {
	Implementation string
	Operation      string
	Variable       string
	Digest         string
	Canonical      []byte
	Placement      Placement
	Materials      []MaterialFile
	// MaterialValues are non-secret values the adapter needs beside the
	// material paths, such as a certificate fingerprint a probe compares.
	MaterialValues map[string]string
	Sudo           string
	Launch         prerequisites.PythonLaunch
	Bundle         prerequisites.BundleLocation
	Area           prerequisites.BundleArea
	Material       map[string]secrets.Material
	Log            func(context.Context, operationstore.LogRecord) error
	Progress       func(context.Context, string, string)
	Diagnostics    func(context.Context, []byte) error
}

type RunResult struct {
	Outcome  string
	Evidence json.RawMessage
}

// Materials lists exactly which bound parts a placement needs on disk. A local
// placement needs none; the SSH arm needs its identity and host key.
func Materials(placement Placement) []MaterialFile {
	var files []MaterialFile
	if placement.PrivateKeyRef != "" {
		files = append(files, MaterialFile{Name: "id", Part: secrets.PrivateKeyPart, Secret: placement.PrivateKeyRef, Variable: "identity"})
	}
	if placement.KnownHostsRef != "" {
		files = append(files, MaterialFile{Name: "known_hosts", Part: secrets.ValuePart, Secret: placement.KnownHostsRef, Variable: "knownHosts"})
	}
	return files
}

// PlacementFor selects the arm one block runs through. The controller is
// local; every other host must author the SSH access the operation binds.
func PlacementFor(machine api.Object, controllerMachine string) (Placement, error) {
	if machine.Name() == controllerMachine {
		return Placement{Connection: ConnectionLocal, Machine: machine.Name()}, nil
	}
	ssh := machine.Spec().Get("access", "ssh")
	if !ssh.Present() {
		return Placement{}, failure("lifecycle.state", "a placement host must be the controller or declare SSH access", "place the work on the controller Machine, or author access.ssh on "+machine.Identity())
	}
	if ssh.Has("auth", "operatorIdentity") {
		return Placement{}, failure("lifecycle.state", "operator SSH identity is unsupported for lifecycle placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if ssh.Has("auth", "passwordRef") {
		return Placement{}, failure("lifecycle.state", "password SSH authentication is unsupported for lifecycle placement", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("auth", "privateKeyRef") {
		return Placement{}, failure("lifecycle.state", "lifecycle placement requires an SSH private key reference", "author access.ssh.auth.privateKeyRef on "+machine.Identity())
	}
	if !ssh.Has("knownHostsRef") {
		return Placement{}, failure("lifecycle.state", "lifecycle placement requires a bound SSH host key", "author access.ssh.knownHostsRef on "+machine.Identity())
	}
	address, err := MachineAddress(machine, ssh.Get("addressRef").Text())
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

// MachineAddress resolves a Machine-local address reference to the value a
// consumer receives: the host IP without its prefix, or the DNS name.
func MachineAddress(machine api.Object, reference string) (string, error) {
	if reference == "" {
		return "", failure("api.required", "the address reference is empty", "name an address on "+machine.Identity())
	}
	for _, address := range machine.Spec().Get("network", "addresses").Items() {
		if address.Get("name").Text() != reference {
			continue
		}
		value := address.Get("address").Text()
		if host, _, found := strings.Cut(value, "/"); found {
			value = host
		}
		if value == "" {
			return "", failure("api.value", "the referenced Machine address is empty", "correct the address on "+machine.Identity())
		}
		return value, nil
	}
	return "", failure("api.reference", "the address reference does not resolve on its Machine", "name an address declared on "+machine.Identity())
}

// AttemptOutcome maps an adapter failure to the outcome it justifies. The
// runner separates a failure it diagnosed — the adapter exited within its
// authorized boundary and said why, so a repeat is the capability's own
// idempotent path — from one it cannot account for, such as a cancellation or
// a lost result. Only a diagnosed failure that is not itself flagged unknown
// leaves the block failed; anything else stays unknown, because a repeat could
// race an effect still in flight.
//
// The distinction matters because nothing retries, destroys or deletes past an
// unknown block: reporting every adapter failure that way strands the context
// with no path forward.
func AttemptOutcome(err error) reconciliation.Outcome {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return reconciliation.OutcomeUnknown
	}
	for _, entry := range reported {
		if entry.Code == "lifecycle.unknown" {
			return reconciliation.OutcomeUnknown
		}
	}
	return reconciliation.OutcomeFailed
}
