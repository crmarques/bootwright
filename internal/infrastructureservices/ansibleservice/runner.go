package ansibleservice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	maxProtocolLine    = 65536
	maxProtocolRecords = 64
	maxMaterialBytes   = 1 << 20
)

var operationPlaybook = map[string]string{
	"apply":   "artifact_server_apply.yml",
	"observe": "artifact_server_observe.yml",
	"destroy": "artifact_server_destroy.yml",
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

// materialFile names one operation-scoped file a secret part is written to.
type materialFile struct {
	name      string
	part      secrets.Part
	secret    string
	mode      uint32
	variable  string
	required  bool
	container string
}

// materialFiles lists exactly which bound parts this invocation needs on disk.
// Anything not listed here never leaves bounded memory.
func materialFiles(request artifactserver.Request) []materialFile {
	var files []materialFile
	if request.TLS != nil {
		files = append(files,
			materialFile{name: "tls.crt", part: secrets.CertificatePart, secret: request.TLS.Secret, mode: 0600, variable: "certificate", required: true},
			materialFile{name: "tls.key", part: secrets.PrivateKeyPart, secret: request.TLS.Secret, mode: 0600, variable: "privateKey", required: true},
		)
	}
	if request.Placement.PrivateKeyRef != "" {
		files = append(files, materialFile{name: "id", part: secrets.PrivateKeyPart, secret: request.Placement.PrivateKeyRef, mode: 0600, variable: "identity", required: true})
	}
	if request.Placement.KnownHostsRef != "" {
		files = append(files, materialFile{name: "known_hosts", part: secrets.ValuePart, secret: request.Placement.KnownHostsRef, mode: 0600, variable: "knownHosts", required: true})
	}
	return files
}

// inventory targets exactly one host. The SSH arm pins host-key checking to
// the bound entry and forbids every ambient identity and password path.
func inventory(request artifactserver.Request, interpreter string, paths map[string]string) map[string]any {
	host := map[string]any{"ansible_python_interpreter": interpreter}
	if request.Placement.Connection == "local" {
		host["ansible_connection"] = "local"
		host["ansible_host"] = "localhost"
	} else {
		host["ansible_connection"] = "ssh"
		host["ansible_host"] = request.Placement.Address
		host["ansible_port"] = request.Placement.Port
		host["ansible_user"] = request.Placement.User
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
		"bootwright_service_host": map[string]any{"hosts": map[string]any{request.Placement.Machine: host}},
	}}}
}

func variables(request artifactserver.Request, digest string, paths map[string]string, sudo string) (map[string]any, error) {
	decoded := map[string]any{}
	canonical, err := request.Canonical()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(canonical, &decoded); err != nil {
		return nil, failure("lifecycle.state", "the frozen artifact-server request could not be prepared")
	}
	material := map[string]any{}
	for _, file := range materialFiles(request) {
		if path, ok := paths[file.name]; ok {
			material[file.variable] = path
		}
	}
	values := map[string]any{
		"bootwright_artifact_server_request":  decoded,
		"bootwright_artifact_server_digest":   digest,
		"bootwright_artifact_server_material": material,
	}
	if sudo != "" {
		values["ansible_become_password"] = sudo
	}
	return values, nil
}

// becomePassword reads the bound escalation secret into the variables file
// rather than an argument or environment variable.
func becomePassword(request artifactserver.Request, material map[string]secrets.Material) (string, error) {
	if request.Placement.SudoPasswordRef == "" {
		return "", nil
	}
	bound, ok := material[request.Placement.SudoPasswordRef]
	if !ok {
		return "", failure("secret.store", "the bound escalation password is not available to this attempt")
	}
	value, ok := bound.Part(secrets.PasswordPart)
	if !ok || len(value) == 0 || len(value) > maxMaterialBytes {
		return "", failure("secret.part", "the bound escalation password has no usable password part")
	}
	return strings.TrimRight(string(value), "\n"), nil
}

func materialBytes(request artifactserver.Request, material map[string]secrets.Material) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, file := range materialFiles(request) {
		bound, ok := material[file.secret]
		if !ok {
			if !file.required {
				continue
			}
			return nil, failure("secret.store", "a bound Secret this operation needs is not available to this attempt")
		}
		value, ok := bound.Part(file.part)
		if !ok || len(value) == 0 || len(value) > maxMaterialBytes {
			return nil, failure("secret.part", "a bound Secret does not carry the part this operation needs")
		}
		out[file.name] = slices.Clone(value)
	}
	return out, nil
}

func failure(code, message string) error {
	return diagnostics.NewFailureWithRemediation(code, message, "", "Preserve the operation and repeat it so its Secret bindings are reopened.")
}
