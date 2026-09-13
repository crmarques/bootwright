package managedservice

import (
	"context"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
)

// MaterialFile names one operation-scoped file a bound Secret part is written
// to. Anything not listed never leaves bounded memory.
type MaterialFile struct {
	Name     string
	Part     secrets.Part
	Secret   string
	Variable string
}

// RunRequest is one authorized adapter invocation, in terms every managed
// service shares. Material is bounded memory owned by the caller; the adapter
// writes it only to operation-scoped files it removes, and never to arguments,
// environment, evidence or logs.
type RunRequest struct {
	Kind      string
	Operation string
	Variable  string
	Digest    string
	Canonical []byte
	Placement Placement
	Materials []MaterialFile
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
}

type RunResult struct {
	Outcome  string
	Evidence json.RawMessage
}

// Materials lists exactly which bound parts an invocation needs on disk. A
// local placement needs none; the SSH arm needs its identity and host key.
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
