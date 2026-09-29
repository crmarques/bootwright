package ansiblerunner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	maxProtocolLine    = 65536
	maxProtocolRecords = 64
	maxMaterialBytes   = 1 << 20
)

// playbookFor answers the entrypoint composition bound to one implementation
// identity and operation. Implementation, not kind, is the key, so two
// implementations of one kind never run each other's automation. The mapping is
// closed: desired state never names a playbook.
func (r Runner) playbookFor(request lifecycle.RunRequest) (string, bool) {
	playbook, ok := r.playbooks[request.Implementation+"/"+request.Operation]
	return playbook, ok
}

type protocolMessage struct {
	Phase    string          `json:"phase"`
	Group    string          `json:"group,omitempty"`
	Status   string          `json:"status,omitempty"`
	Outcome  string          `json:"outcome,omitempty"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

// readProtocol decodes the adapter's result channel. Every record is bounded
// and strictly shaped; adapter prose is never a product result.
func readProtocol(reader io.Reader, messages chan<- protocolMessage) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxProtocolLine)
	for count := 0; scanner.Scan(); count++ {
		if count >= maxProtocolRecords {
			return errors.New("protocol limit")
		}
		var message protocolMessage
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&message) != nil || decoder.More() {
			return errors.New("protocol record")
		}
		switch message.Phase {
		case "loaded":
			if message.Group != "" || message.Status != "" || message.Outcome != "" || len(message.Evidence) != 0 {
				return errors.New("protocol record")
			}
		case "group":
			if message.Group == "" || message.Status == "" || message.Outcome != "" || len(message.Evidence) != 0 {
				return errors.New("protocol record")
			}
		case "completed":
			if message.Outcome != "changed" && message.Outcome != "unchanged" || len(message.Evidence) == 0 {
				return errors.New("protocol record")
			}
		default:
			return errors.New("protocol phase")
		}
		messages <- message
	}
	return scanner.Err()
}

// inventory targets exactly one host. The SSH arm pins host-key checking to
// the bound entry and forbids every ambient identity and password path.
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
			"-o", "UserKnownHostsFile=" + paths["known_hosts"],
			"-o", "StrictHostKeyChecking=yes",
			"-o", "IdentitiesOnly=yes",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
			"-o", "GSSAPIAuthentication=no",
			"-o", "IdentityAgent=none",
			"-o", "BatchMode=yes",
		}, " ")
	}
	return map[string]any{"all": map[string]any{"children": map[string]any{
		"bootwright_target": map[string]any{"hosts": map[string]any{placement.Machine: host}},
	}}}
}

// variables carries the frozen request and the paths its material was written
// to. Material values themselves never enter this file, and neither does an
// escalation password: a placement connects as root and never escalates.
func variables(request lifecycle.RunRequest, paths map[string]string) (map[string]any, error) {
	decoded := map[string]any{}
	if err := json.Unmarshal(request.Canonical, &decoded); err != nil {
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
const (
	bindingRemediation = "repeat the operation so its Secret bindings are reopened"
	outputRemediation  = "read the adapter output retained beside this attempt's log"
	setupRemediation   = "run bootwright setup"
)

func failure(code, message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", remediation)
}
