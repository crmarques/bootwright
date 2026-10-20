package ceph

import (
	"bytes"
	"maps"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const maxSMBSecret = 1024

func smbSecretValue(e lifecycle.Execution, object string, b SMBBinding) error {
	value, bound := e.Material[b.SecretRef].Part(secrets.ValuePart)
	defer clear(value)
	if !bound {
		return failure("lifecycle.state", "a required Ceph credential component is not bound", "")
	}
	if len(value) == 0 || len(value) > maxSMBSecret || !utf8.Valid(value) || bytes.ContainsAny(value, "\x00\r\n") {
		return failure("lifecycle.unsupported", "the Secret "+b.SecretRef+" that StorageSMBResource "+object+" binds at "+b.Path+" is not one line of 1 to 1024 bytes of UTF-8 text",
			"store the password in Secret "+b.SecretRef+" as one line of 1 to 1024 bytes with bootwright secret set, then apply again")
	}
	return nil
}

func smbMaterial(e lifecycle.Execution, r SMBRequest, scope ClusterScope, operation string, readers DependencyProofs) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	cleanup := func() {}
	if len(scope.Hosts) == 0 {
		return e, nil, cleanup, failure("lifecycle.state", "Ceph has no frozen hosts", "")
	}
	files := []lifecycle.MaterialFile{{Name: "fleet-key", Secret: scope.Hosts[0].SSHKeyRef, Part: secrets.PrivateKeyPart, Variable: "sshKey"}, {Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"}}
	if operation == "apply" {
		for i, b := range r.Bindings {
			if err := smbSecretValue(e, r.Object, b); err != nil {
				return e, nil, cleanup, err
			}
			files = append(files, lifecycle.MaterialFile{Name: "smb-secret-" + strconv.Itoa(i), Secret: b.SecretRef, Part: secrets.ValuePart, Variable: "smbSecret" + strconv.Itoa(i)})
		}
	}
	known := strings.Builder{}
	for _, h := range scope.Hosts {
		key, err := provedHostKey(h, e, readers)
		if err != nil {
			return e, nil, cleanup, err
		}
		known.WriteString(key.Line(h.Address, h.Port))
		known.WriteByte('\n')
	}
	bound := maps.Clone(e.Material)
	if bound == nil {
		bound = map[string]secrets.Material{}
	}
	bound[knownHostsMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(known.String())})
	e.Material = bound
	return e, files, func() { bound[knownHostsMaterial].Clear() }, nil
}
