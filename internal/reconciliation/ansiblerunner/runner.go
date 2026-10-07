package ansiblerunner

import (
	"bytes"
	"encoding/json"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const maxMaterialBytes = 1 << 20

// playbookFor answers the entrypoint composition bound to one implementation
// identity and operation. Implementation, not kind, is the key, so two
// implementations of one kind never run each other's automation. The mapping is
// closed: desired state never names a playbook.
func (r Runner) playbookFor(request lifecycle.RunRequest) (string, bool) {
	playbook, ok := r.playbooks[request.Implementation+"/"+request.Operation]
	return playbook, ok
}

// inventory targets exactly one host. The SSH arm reads only the job's own
// client configuration, pins host-key checking to the bound entry, forbids
// every ambient identity, password, proxy and known-hosts path, and ends a
// connection whose peer stops answering.
func inventory(placement machineref.Placement, interpreter string, paths map[string]string) map[string]any {
	host := map[string]any{"ansible_python_interpreter": interpreter}
	if placement.Local() {
		host["ansible_connection"] = "local"
		host["ansible_host"] = "localhost"
	} else {
		host["ansible_connection"] = "ssh"
		host["ansible_host"] = placement.Address
		host["ansible_port"] = placement.Port
		host["ansible_user"] = placement.User
		host["ansible_ssh_private_key_file"] = paths["id"]
		host["ansible_python_interpreter"] = "/usr/bin/python3"
		host["ansible_ssh_common_args"] = strings.Join([]string{
			"-F", paths["ssh_config"],
			"-o", "UserKnownHostsFile=" + paths["known_hosts"],
			"-o", "GlobalKnownHostsFile=none",
			"-o", "KnownHostsCommand=none",
			"-o", "VerifyHostKeyDNS=no",
			"-o", "UpdateHostKeys=no",
			"-o", "CheckHostIP=no",
			"-o", "StrictHostKeyChecking=yes",
			"-o", "IdentitiesOnly=yes",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "GSSAPIAuthentication=no",
			"-o", "HostbasedAuthentication=no",
			"-o", "IdentityAgent=none",
			"-o", "ForwardAgent=no",
			"-o", "ProxyCommand=none",
			"-o", "ProxyJump=none",
			"-o", "BatchMode=yes",
			"-o", "ServerAliveInterval=15",
			"-o", "ServerAliveCountMax=3",
		}, " ")
	}
	return map[string]any{"all": map[string]any{"children": map[string]any{
		"bootwright_target": map[string]any{"hosts": map[string]any{placement.Machine: host}},
	}}}
}

// variables carries the frozen request and the paths its material was written
// to. Material values themselves never enter this file, and neither does an
// escalation password: a placement connects as root and never escalates. Each
// number keeps its canonical text, which a float64 rounds above 2^53.
func variables(request lifecycle.RunRequest, paths map[string]string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(request.Canonical))
	decoder.UseNumber()
	decoded := map[string]any{}
	if decoder.Decode(&decoded) != nil || decoded == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, failure("lifecycle.state", "the frozen adapter request could not be prepared", "")
	}
	material := map[string]any{}
	for _, file := range request.Materials {
		if path, ok := paths[file.Name]; ok {
			material[file.Variable] = path
		}
	}
	for name, value := range request.MaterialValues {
		material[name] = value
	}
	return map[string]any{
		request.Variable + "_request":  decoded,
		request.Variable + "_digest":   request.Digest,
		request.Variable + "_material": material,
	}, nil
}

func materialBytes(request lifecycle.RunRequest) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, file := range request.Materials {
		bound, ok := request.Material[file.Secret]
		if !ok {
			return nil, failure("secret.store", "a bound Secret this operation needs is not available to this attempt", bindingRemediation)
		}
		value, ok := bound.Part(file.Part)
		if !ok || len(value) == 0 || len(value) > maxMaterialBytes {
			return nil, failure("secret.part", "a bound Secret does not carry the part this operation needs", bindingRemediation)
		}
		out[file.Name] = slices.Clone(value)
	}
	return out, nil
}

// Each adapter failure names the recovery its own cause needs. One shared
// remediation was wrong for every failure that had nothing to do with Secrets.
// A failure the adapter's own output explains names what its request says,
// because only the caller knows where that output is named.
const (
	bindingRemediation = "repeat the operation so its Secret bindings are reopened"
	setupRemediation   = "run bootwright setup"
)

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
