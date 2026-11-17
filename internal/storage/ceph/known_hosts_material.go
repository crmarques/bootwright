package ceph

import (
	"maps"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

type knownHostsLines struct {
	lines strings.Builder
	gap   string
}

func (k *knownHostsLines) add(e lifecycle.Execution, hosts []Host, security *Security, readers DependencyProofs) error {
	for _, h := range hosts {
		key, err := fleetHostKey(h, security, e, readers)
		if err != nil {
			return err
		}
		k.lines.WriteString(key.Line(h.Address, h.Port) + k.gap)
	}
	return nil
}

func (k *knownHostsLines) bind(e lifecycle.Execution) (lifecycle.Execution, lifecycle.MaterialFile, func()) {
	bound := maps.Clone(e.Material)
	if bound == nil {
		bound = map[string]secrets.Material{}
	}
	bound[knownHostsMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(k.lines.String())})
	e.Material = bound
	return e, lifecycle.MaterialFile{Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"}, func() { bound[knownHostsMaterial].Clear() }
}

func bindKnownHosts(e lifecycle.Execution, hosts []Host, security *Security, readers DependencyProofs) (lifecycle.Execution, lifecycle.MaterialFile, func(), error) {
	known := knownHostsLines{gap: "\n"}
	if err := known.add(e, hosts, security, readers); err != nil {
		return e, lifecycle.MaterialFile{}, func() {}, err
	}
	bound, file, cleanup := known.bind(e)
	return bound, file, cleanup, nil
}

func fleetKeyFile(ref string) lifecycle.MaterialFile {
	return lifecycle.MaterialFile{Name: "fleet-key", Secret: ref, Part: secrets.PrivateKeyPart, Variable: "sshKey"}
}
