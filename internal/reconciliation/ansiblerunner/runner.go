package ansiblerunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
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

func checkReadOnly(request lifecycle.RunRequest) error {
	lend := request.Operation == lifecycle.LendOperation
	if request.ReadOnly && request.Operation != lifecycle.ObserveOperation && !lend {
		return failure("lifecycle.state", "a read-only invocation may run only its capability's observation", "")
	}
	if request.Unrecorded && len(request.Outputs) != 0 && !lend {
		return failure("lifecycle.state", "an invocation no operation records may take over no material, so this observation cannot run outside an operation", "")
	}
	return nil
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

func phaseInventory(request lifecycle.RunRequest, paths map[string]string) (map[string]any, error) {
	value := inventory(request.Placement, request.Launch.Shim, paths)
	if request.NodeCount == 0 {
		return value, nil
	}
	if !request.Placement.Local() || request.NodeCount < 0 || request.NodeCount > 512 || request.Parallelism < 1 || request.Parallelism > 16 {
		return nil, failure("lifecycle.state", "per-node inventory requires bounded controller-local phases", "plan the operation again")
	}
	hosts := map[string]any{}
	for index := 0; index < request.NodeCount; index++ {
		hosts[fmt.Sprintf("bootwright_node_%03d", index)] = map[string]any{
			"ansible_connection": "local", "ansible_host": "localhost", "ansible_python_interpreter": request.Launch.Shim,
			"bootwright_node_index": index, "bootwright_controller_host": request.Placement.Machine,
		}
	}
	value["all"].(map[string]any)["children"].(map[string]any)["bootwright_nodes"] = map[string]any{"hosts": hosts}
	return value, nil
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

func checkHostKey(request lifecycle.RunRequest) (string, error) {
	placement := request.Placement
	if placement.Local() {
		return "", nil
	}
	pinned := slices.IndexFunc(request.Materials, func(file lifecycle.MaterialFile) bool { return file.Name == "known_hosts" })
	if placement.KnownHostsRef == "" || pinned < 0 || request.Materials[pinned].Secret != placement.KnownHostsRef || request.Materials[pinned].Part != secrets.ValuePart {
		return "", failure("lifecycle.state", "an SSH placement connects only against the host key its own access.ssh.knownHostsRef binds",
			"author access.ssh.knownHostsRef on Machine/"+placement.Machine)
	}
	bound, ok := request.Material[placement.KnownHostsRef]
	if !ok {
		return "", failure("secret.store", "a bound Secret this operation needs is not available to this attempt", lifecycle.ReopenBindings(request.Context, request.Repeat))
	}
	value, ok := bound.Part(secrets.ValuePart)
	defer clear(value)
	if !ok || len(value) == 0 || len(value) > maxMaterialBytes {
		return "", failure("secret.part", "a bound Secret does not carry the part this operation needs", lifecycle.ReopenBindings(request.Context, request.Repeat))
	}
	key, err := trust.ParseKnownHostsLine(string(value), placement.Address, placement.Port)
	if err != nil {
		reason := err.Error()
		if reported := diagnostics.Of(err); len(reported) != 0 {
			reason = reported[0].Message
		}
		token := trust.HostToken(placement.Address, placement.Port)
		return "", failure("trust.identity",
			"the SSH host key Secret "+placement.KnownHostsRef+" of Machine/"+placement.Machine+" is not one known-hosts entry for "+token+
				" ("+reason+"), so nothing connects to that host",
			"author access.ssh.knownHostsRef on Machine/"+placement.Machine+" as one known-hosts entry for "+token)
	}
	return key.Line(placement.Address, placement.Port), nil
}

func materialBytes(request lifecycle.RunRequest) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, file := range request.Materials {
		bound, ok := request.Material[file.Secret]
		if !ok {
			return nil, failure("secret.store", "a bound Secret this operation needs is not available to this attempt", lifecycle.ReopenBindings(request.Context, request.Repeat))
		}
		value, ok := bound.Part(file.Part)
		if !ok || len(value) == 0 || len(value) > maxMaterialBytes {
			return nil, failure("secret.part", "a bound Secret does not carry the part this operation needs", lifecycle.ReopenBindings(request.Context, request.Repeat))
		}
		out[file.Name] = slices.Clone(value)
	}
	return out, nil
}

// Each adapter failure names the recovery its own cause needs. One shared
// remediation was wrong for every failure that had nothing to do with Secrets.
// A failure the adapter's own output explains names what its request says,
// because only the caller knows where that output is named.
const setupRemediation = "run bootwright controller install"

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
