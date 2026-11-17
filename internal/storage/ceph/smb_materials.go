package ceph

import (
	"bytes"
	"strconv"
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
		return failure("lifecycle.unsupported", "the Secret "+b.SecretRef+" that CephSMBResource "+object+" binds at "+b.Path+" is not one line of 1 to 1024 bytes of UTF-8 text",
			"store the password in Secret "+b.SecretRef+" as one line of 1 to 1024 bytes with bootwright secret set, then apply again")
	}
	return nil
}

func smbMaterial(e lifecycle.Execution, r SMBRequest, scope ClusterScope, operation string, readers DependencyProofs) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	cleanup := func() {}
	if len(scope.Hosts) == 0 {
		return e, nil, cleanup, failure("lifecycle.state", "Ceph has no frozen hosts", "")
	}
	bindings := []lifecycle.MaterialFile{}
	if operation == "apply" {
		for i, b := range r.Bindings {
			if err := smbSecretValue(e, r.Object, b); err != nil {
				return e, nil, cleanup, err
			}
			bindings = append(bindings, lifecycle.MaterialFile{Name: "smb-secret-" + strconv.Itoa(i), Secret: b.SecretRef, Part: secrets.ValuePart, Variable: "smbSecret" + strconv.Itoa(i)})
		}
	}
	bound, knownFile, clearKnown, err := bindKnownHosts(e, scope.Hosts, scope.Security, readers)
	if err != nil {
		return e, nil, cleanup, err
	}
	return bound, append([]lifecycle.MaterialFile{fleetKeyFile(scope.Hosts[0].SSHKeyRef), knownFile}, bindings...), clearKnown, nil
}
