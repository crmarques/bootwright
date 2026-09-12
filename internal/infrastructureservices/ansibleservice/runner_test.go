package ansibleservice

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/secrets"
)

func localRequest() artifactserver.Request {
	return artifactserver.Request{
		BindAddress: "192.0.2.1",
		ContentRoot: "/var/lib/bootwright-services/ctx-1/artifact-server/lab",
		Egress:      artifactserver.Egress{NoProxy: []string{}},
		Endpoints:   []artifactserver.Endpoint{{Address: "192.0.2.1", Listener: "https", Name: "ip-https"}},
		Identity:    artifactserver.Identity{Block: "artifact-server-lab", Context: "ctx-1", Service: "lab"},
		Image:       "registry.example.test/nginx@sha256:" + strings.Repeat("a", 64),
		Listeners:   []artifactserver.Listener{{Name: "https", Port: 8443, Protocol: "https"}},
		Placement:   artifactserver.Placement{Connection: "local", Machine: "bastion"},
		TLS:         &artifactserver.TLS{MinVersion: "TLSv1.2", Secret: "artifact-server-tls"},
		Unit:        "bootwright-ctx-1-artifacts-lab",
		Version:     "artifact-server-nginx-v1",
	}
}

func sshRequest() artifactserver.Request {
	request := localRequest()
	request.Placement = artifactserver.Placement{
		Address: "192.0.2.9", Connection: "ssh", KnownHostsRef: "host-key",
		Machine: "services", Port: 2222, PrivateKeyRef: "services-key",
		SudoPasswordRef: "services-sudo", User: "operator",
	}
	return request
}

func material() map[string]secrets.Material {
	return map[string]secrets.Material{
		"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.CertificatePart: []byte("CERTIFICATE"), secrets.PrivateKeyPart: []byte("PRIVATE"),
		}),
		"services-key":  secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("SSHKEY")}),
		"host-key":      secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("host ssh-ed25519 AAAA")}),
		"services-sudo": secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte("escalate\n")}),
	}
}

func TestMaterialFilesCoverExactlyTheBoundParts(t *testing.T) {
	local := materialFiles(localRequest())
	if len(local) != 2 {
		t.Fatalf("local material files = %+v", local)
	}
	for _, file := range local {
		if file.mode != 0600 {
			t.Fatalf("%s uses mode %o", file.name, file.mode)
		}
	}
	remote := materialFiles(sshRequest())
	names := []string{}
	for _, file := range remote {
		names = append(names, file.name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"id", "known_hosts", "tls.crt", "tls.key"}) {
		t.Fatalf("ssh material files = %v", names)
	}
	plain := localRequest()
	plain.TLS = nil
	if len(materialFiles(plain)) != 0 {
		t.Fatal("an HTTP-only request still demands serving material")
	}
}

func TestMaterialBytesRefuseMissingOrUnusableParts(t *testing.T) {
	values, err := materialBytes(localRequest(), material())
	if err != nil || string(values["tls.crt"]) != "CERTIFICATE" || string(values["tls.key"]) != "PRIVATE" {
		t.Fatalf("material = %v (%v)", values, err)
	}
	if _, err := materialBytes(localRequest(), nil); err == nil {
		t.Fatal("absent bound material was accepted")
	}
	empty := map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: nil, secrets.PrivateKeyPart: []byte("PRIVATE"),
	})}
	if _, err := materialBytes(localRequest(), empty); err == nil {
		t.Fatal("an empty part was accepted")
	}
}

func TestVariablesCarryPathsNotMaterial(t *testing.T) {
	paths := map[string]string{"tls.crt": "/job/tls.crt", "tls.key": "/job/tls.key"}
	values, err := variables(localRequest(), strings.Repeat("d", 64), paths, "escalate")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"CERTIFICATE", "PRIVATE", "BEGIN"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("the variables carry %q", forbidden)
		}
	}
	if !strings.Contains(string(encoded), "/job/tls.key") {
		t.Fatal("the variables do not name the prepared material file")
	}
	if !strings.Contains(string(encoded), `"ansible_become_password":"escalate"`) {
		t.Fatal("the escalation password did not reach the variables file")
	}
	if values["bootwright_artifact_server_digest"] != strings.Repeat("d", 64) {
		t.Fatal("the frozen request digest was not passed to the adapter")
	}
}

func TestBecomePasswordRequiresItsBoundSecret(t *testing.T) {
	value, err := becomePassword(sshRequest(), material())
	if err != nil || value != "escalate" {
		t.Fatalf("password = %q (%v)", value, err)
	}
	if _, err := becomePassword(sshRequest(), map[string]secrets.Material{}); err == nil {
		t.Fatal("a missing escalation secret was accepted")
	}
	if value, err := becomePassword(localRequest(), material()); err != nil || value != "" {
		t.Fatalf("a request without escalation returned %q (%v)", value, err)
	}
}

func TestInventoryPinsTheSSHIdentityAndHostKey(t *testing.T) {
	paths := map[string]string{"id": "/job/id", "known_hosts": "/job/known_hosts"}
	value := inventory(sshRequest(), "/interpreter", paths)
	host := value["all"].(map[string]any)["children"].(map[string]any)["bootwright_service_host"].(map[string]any)["hosts"].(map[string]any)["services"].(map[string]any)
	if host["ansible_connection"] != "ssh" || host["ansible_host"] != "192.0.2.9" || host["ansible_port"] != 2222 || host["ansible_user"] != "operator" {
		t.Fatalf("ssh host = %+v", host)
	}
	if host["ansible_ssh_private_key_file"] != "/job/id" {
		t.Fatalf("identity file = %v", host["ansible_ssh_private_key_file"])
	}
	arguments, _ := host["ansible_ssh_common_args"].(string)
	for _, required := range []string{
		"UserKnownHostsFile=/job/known_hosts", "StrictHostKeyChecking=yes", "IdentitiesOnly=yes",
		"PasswordAuthentication=no", "IdentityAgent=none", "BatchMode=yes",
	} {
		if !strings.Contains(arguments, required) {
			t.Fatalf("ssh arguments = %q, missing %q", arguments, required)
		}
	}
	local := inventory(localRequest(), "/interpreter", nil)
	host = local["all"].(map[string]any)["children"].(map[string]any)["bootwright_service_host"].(map[string]any)["hosts"].(map[string]any)["bastion"].(map[string]any)
	if host["ansible_connection"] != "local" || host["ansible_python_interpreter"] != "/interpreter" {
		t.Fatalf("local host = %+v", host)
	}
	if _, present := host["ansible_ssh_common_args"]; present {
		t.Fatal("local placement carries SSH arguments")
	}
}

func TestProtocolAcceptsOnlyItsThreeBoundedPhases(t *testing.T) {
	valid := strings.Join([]string{
		`{"phase":"loaded"}`,
		`{"phase":"group","group":"pull-image","status":"running"}`,
		`{"phase":"completed","outcome":"changed","evidence":{"absent":false}}`,
	}, "\n")
	messages := make(chan protocolMessage, 8)
	done := make(chan error, 1)
	go func() { done <- readProtocol(strings.NewReader(valid), messages); close(messages) }()
	phases := []string{}
	for message := range messages {
		phases = append(phases, message.Phase)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(phases, []string{"loaded", "group", "completed"}) {
		t.Fatalf("phases = %v", phases)
	}
	for name, line := range map[string]string{
		"unknown phase":   `{"phase":"native"}`,
		"unknown field":   `{"phase":"loaded","extra":1}`,
		"loaded evidence": `{"phase":"loaded","evidence":{"a":1}}`,
		"group no status": `{"phase":"group","group":"pull-image"}`,
		"bad outcome":     `{"phase":"completed","outcome":"done","evidence":{"a":1}}`,
		"no evidence":     `{"phase":"completed","outcome":"changed"}`,
		"not json":        `not json`,
	} {
		t.Run(name, func(t *testing.T) {
			drain := make(chan protocolMessage, 8)
			go func() {
				for range drain {
				}
			}()
			err := readProtocol(strings.NewReader(line), drain)
			close(drain)
			if err == nil {
				t.Fatalf("an invalid protocol record was accepted: %s", line)
			}
		})
	}
}

func TestProtocolIsBoundedInRecordsAndLength(t *testing.T) {
	many := strings.Repeat(`{"phase":"loaded"}`+"\n", maxProtocolRecords+2)
	drain := make(chan protocolMessage, maxProtocolRecords+4)
	go func() {
		for range drain {
		}
	}()
	err := readProtocol(strings.NewReader(many), drain)
	close(drain)
	if err == nil {
		t.Fatal("an unbounded protocol stream was accepted")
	}
}
