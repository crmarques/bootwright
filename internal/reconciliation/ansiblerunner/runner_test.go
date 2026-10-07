package ansiblerunner

import (
	"encoding/json"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func localRequest() lifecycle.RunRequest {
	return lifecycle.RunRequest{
		Context: "context-canary", Block: "block-canary", Description: "Serve description-canary",
		Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
		Digest:    strings.Repeat("d", 64),
		Canonical: []byte(`{"unit":"bootwright-lab-artifacts-lab"}`),
		Placement: machineref.Placement{Connection: "local", Machine: "controller"},
		Materials: []lifecycle.MaterialFile{
			{Name: "tls.crt", Part: secrets.CertificatePart, Secret: "artifact-server-tls", Variable: "certificate"},
			{Name: "tls.key", Part: secrets.PrivateKeyPart, Secret: "artifact-server-tls", Variable: "privateKey"},
		},
		Material: material(),
	}
}

func sshRequest() lifecycle.RunRequest {
	request := localRequest()
	request.Placement = machineref.Placement{
		Address: "192.0.2.9", Connection: "ssh", KnownHostsRef: "host-key",
		Machine: "services", Port: 2222, PrivateKeyRef: "services-key", User: "root",
	}
	request.Materials = append(slices.Clone(request.Materials), lifecycle.Materials(request.Placement)...)
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

// Every implementation reaches the same fixed entrypoint set, and only through
// it: an implementation and operation the map does not name has no playbook.
// Keying on the implementation is what lets two implementations of one kind
// carry different automation without either reaching the other's.
func TestEveryImplementationAndOperationBindsOneFixedEntrypoint(t *testing.T) {
	bound := map[string]string{}
	for _, implementation := range []string{
		"artifact-server-nginx-v1", "proxy-squid-v1", "dns-server-dnsmasq-v1", "ntp-server-chrony-v1",
	} {
		for _, operation := range []string{"apply", "observe", "destroy"} {
			bound[implementation+"/"+operation] = "infrastructureservices/" + implementation + "_" + operation + ".yml"
		}
	}
	runner := New(bound)
	for key, want := range bound {
		implementation, operation, _ := strings.Cut(key, "/")
		playbook, ok := runner.playbookFor(lifecycle.RunRequest{Implementation: implementation, Operation: operation})
		if !ok || playbook != want || strings.HasPrefix(playbook, "/") {
			t.Fatalf("%s resolved to %q", key, playbook)
		}
	}
	for _, request := range []lifecycle.RunRequest{
		{Implementation: "registry-quay-v1", Operation: "apply"},
		{Implementation: "artifact-server-nginx-v1", Operation: "reconcile"},
		{Implementation: "ArtifactServer", Operation: "apply"},
		{Implementation: "", Operation: ""},
	} {
		if _, ok := runner.playbookFor(request); ok {
			t.Fatalf("%s/%s resolved to a playbook", request.Implementation, request.Operation)
		}
	}
}

func TestMaterialFilesCoverExactlyTheBoundParts(t *testing.T) {
	names := []string{}
	for _, file := range sshRequest().Materials {
		names = append(names, file.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"id", "known_hosts", "tls.crt", "tls.key"}) {
		t.Fatalf("ssh material files = %v", names)
	}
	plain := localRequest()
	plain.Materials = lifecycle.Materials(plain.Placement)
	if len(plain.Materials) != 0 {
		t.Fatal("a local placement without serving material still demands a file")
	}
}

func TestMaterialBytesRefuseMissingOrUnusableParts(t *testing.T) {
	values, err := materialBytes(localRequest())
	if err != nil || string(values["tls.crt"]) != "CERTIFICATE" || string(values["tls.key"]) != "PRIVATE" {
		t.Fatalf("material = %v (%v)", values, err)
	}
	absent := localRequest()
	absent.Material = nil
	if _, err := materialBytes(absent); err == nil {
		t.Fatal("absent bound material was accepted")
	}
	empty := localRequest()
	empty.Material = map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: nil, secrets.PrivateKeyPart: []byte("PRIVATE"),
	})}
	if _, err := materialBytes(empty); err == nil {
		t.Fatal("an empty part was accepted")
	}
}

func TestVariablesCarryPathsNotMaterial(t *testing.T) {
	paths := map[string]string{"tls.crt": "/job/tls.crt", "tls.key": "/job/tls.key", "id": "/job/id", "known_hosts": "/job/known_hosts"}
	request := sshRequest()
	request.MaterialValues = map[string]string{"fingerprint": strings.Repeat("f", 64)}
	values, err := variables(request, paths)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"CERTIFICATE", "PRIVATE", "BEGIN", "SSHKEY", "ansible_become_password", "escalate", "context-canary", "block-canary", "description-canary"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("the variables carry %q", forbidden)
		}
	}
	if !strings.Contains(string(encoded), "/job/tls.key") {
		t.Fatal("the variables do not name the prepared material file")
	}
	if values["bootwright_artifact_server_digest"] != strings.Repeat("d", 64) {
		t.Fatal("the frozen request digest was not passed to the adapter")
	}
}

func TestInventoryPinsTheSSHIdentityAndHostKey(t *testing.T) {
	paths := map[string]string{"id": "/job/id", "known_hosts": "/job/known_hosts"}
	value := inventory(sshRequest().Placement, "/interpreter", paths)
	host := value["all"].(map[string]any)["children"].(map[string]any)["bootwright_target"].(map[string]any)["hosts"].(map[string]any)["services"].(map[string]any)
	if host["ansible_connection"] != "ssh" || host["ansible_host"] != "192.0.2.9" || host["ansible_port"] != 2222 || host["ansible_user"] != "root" {
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
	local := inventory(localRequest().Placement, "/interpreter", nil)
	host = local["all"].(map[string]any)["children"].(map[string]any)["bootwright_target"].(map[string]any)["hosts"].(map[string]any)["controller"].(map[string]any)
	if host["ansible_connection"] != "local" || host["ansible_python_interpreter"] != "/interpreter" {
		t.Fatalf("local host = %+v", host)
	}
	if _, present := host["ansible_ssh_common_args"]; present {
		t.Fatal("local placement carries SSH arguments")
	}
}

// Every ssh the SSH arm starts reads only the job's generated configuration and
// takes no ambient known hosts, proxy, agent or forwarding, and a peer that
// stops answering ends the connection instead of hanging the run.
func TestTheSSHArmUsesAGeneratedConfigurationAndKeepalives(t *testing.T) {
	paths := map[string]string{"id": "/job/id", "known_hosts": "/job/known_hosts", "ssh_config": "/job/ssh_config"}
	value := inventory(sshRequest().Placement, "/interpreter", paths)
	host := value["all"].(map[string]any)["children"].(map[string]any)["bootwright_target"].(map[string]any)["hosts"].(map[string]any)["services"].(map[string]any)
	arguments := strings.Fields(host["ansible_ssh_common_args"].(string))
	if len(arguments) < 2 || arguments[0] != "-F" || arguments[1] != "/job/ssh_config" {
		t.Fatalf("ssh arguments = %q, not led by the generated configuration", arguments)
	}
	options := map[string]bool{}
	for index := 2; index < len(arguments); index += 2 {
		if arguments[index] != "-o" || index+1 >= len(arguments) {
			t.Fatalf("ssh arguments = %q, not -o pairs after -F", arguments)
		}
		options[arguments[index+1]] = true
	}
	for _, required := range []string{
		"UserKnownHostsFile=/job/known_hosts", "GlobalKnownHostsFile=none", "KnownHostsCommand=none",
		"VerifyHostKeyDNS=no", "UpdateHostKeys=no", "CheckHostIP=no", "StrictHostKeyChecking=yes",
		"IdentitiesOnly=yes", "IdentityAgent=none", "PasswordAuthentication=no", "KbdInteractiveAuthentication=no",
		"GSSAPIAuthentication=no", "HostbasedAuthentication=no", "ForwardAgent=no", "ProxyCommand=none",
		"ProxyJump=none", "BatchMode=yes", "ServerAliveInterval=15", "ServerAliveCountMax=3",
	} {
		if !options[required] {
			t.Fatalf("ssh arguments = %q, missing %q", arguments, required)
		}
	}
}

func TestProtocolAcceptsOnlyItsBoundedPhases(t *testing.T) {
	valid := strings.Join([]string{
		`{"phase":"loaded"}`,
		`{"phase":"group","group":"pull-image","status":"running"}`,
		`{"phase":"refused","reason":"identity-mismatch"}`,
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
	if !slices.Equal(phases, []string{"loaded", "group", "refused", "completed"}) {
		t.Fatalf("phases = %v", phases)
	}
	for name, line := range map[string]string{
		"unknown phase":     `{"phase":"native"}`,
		"unknown field":     `{"phase":"loaded","extra":1}`,
		"loaded evidence":   `{"phase":"loaded","evidence":{"a":1}}`,
		"loaded reason":     `{"phase":"loaded","reason":"identity-mismatch"}`,
		"group no status":   `{"phase":"group","group":"pull-image"}`,
		"group reason":      `{"phase":"group","group":"pull-image","status":"running","reason":"identity-mismatch"}`,
		"bad outcome":       `{"phase":"completed","outcome":"done","evidence":{"a":1}}`,
		"no evidence":       `{"phase":"completed","outcome":"changed"}`,
		"completed reason":  `{"phase":"completed","outcome":"changed","evidence":{"a":1},"reason":"identity-mismatch"}`,
		"refused no reason": `{"phase":"refused"}`,
		"refused outcome":   `{"phase":"refused","reason":"identity-mismatch","outcome":"changed"}`,
		"refused group":     `{"phase":"refused","reason":"identity-mismatch","group":"read-state"}`,
		"not json":          `not json`,
		"closing brace":     `{"phase":"loaded"}}`,
		"closing bracket":   `{"phase":"loaded"}]`,
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
