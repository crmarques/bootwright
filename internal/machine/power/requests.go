package power

import (
	"bytes"
	"encoding/json"
	machineref "github.com/crmarques/bootwright/internal/machine"
)

// PowerRequest is what one invocation asks for.
type PowerRequest struct {
	ContextName      string
	Name             string
	Verb             string
	Force            bool
	SkipConfirmation bool
}

// Identity names the context and Machine one request acts on.
type Identity struct {
	Context string `json:"context"`
	Object  string `json:"object"`
}

// Controller is the Redfish endpoint this Machine is managed through, the
// declaration whose credential answers it, and how its transport is verified.
// No material is named here.
type Controller struct {
	CredentialsRef string `json:"credentialsRef"`
	Endpoint       string `json:"endpoint"`
	// TLSVerify and TrustBundleRef carry the Machine's declared trust to the
	// adapter: a controller whose certificate an internal authority issued is
	// verified against the bundle naming that authority alone, one with no
	// bundle against the system trust store, and one declared unverified is
	// not verified at all.
	TLSVerify      bool   `json:"tlsVerify"`
	TrustBundleRef string `json:"trustBundleRef,omitempty"`
}

// Request is the complete frozen intent for one power operation. It carries no
// secret value: the controller credential reaches the adapter as an
// operation-scoped file at execution.
type Request struct {
	Controller Controller           `json:"controller"`
	Force      bool                 `json:"force"`
	Identity   Identity             `json:"identity"`
	Placement  machineref.Placement `json:"placement"`
	Verb       string               `json:"verb"`
	// Version is the request shape the adapter validates before it acts, so
	// automation never runs against a request it does not understand.
	Version string `json:"version"`
}

// Result is what the operation proved. Power is the state the controller
// reported after the operation settled, never the state it was asked for. A
// run that fails once its runtime is lent proves nothing, so it returns,
// beside its error, a Result naming only where its adapter output is retained.
type Result struct {
	Context  string
	Machine  string
	Verb     string
	Power    string
	Previous string
	Changed  bool
	// LogLocation and Logs name where this run's adapter output was retained:
	// the directory on this host, and the file inside the context's own state.
	// They are troubleshooting material, never evidence of what happened.
	LogLocation string
	Logs        []string
}

// Canonical encodes the request exactly as the adapter consumes it, refusing
// anything a later reader could interpret differently.
func (r Request) Canonical() ([]byte, error) {
	return canonicalBytes(r, "power request")
}

// canonicalBytes encodes one frozen intent and proves the bytes survive a
// decode and re-encode unchanged, so what the adapter reads is what the digest
// covers.
func canonicalBytes(value any, subject string) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, failure("lifecycle.state", "the "+subject+" cannot be encoded", "")
	}
	var probe map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&probe); err != nil {
		return nil, failure("lifecycle.state", "the "+subject+" cannot be decoded", "")
	}
	reencoded, err := json.Marshal(probe)
	if err != nil || !bytes.Equal(data, reencoded) {
		return nil, failure("lifecycle.state", "the "+subject+" is not canonical", "")
	}
	return data, nil
}
