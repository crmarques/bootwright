package infrastructureservices

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func servingSecret(source api.Value) api.Object {
	return obj(api.Secret, "serving", m("type", "tlsCertificate", "source", source))
}

func generatedSource(dnsNames, ipAddresses []string) api.Value {
	return m("generated", m("commonName", "host.example.test", "dnsNames", api.StringList(dnsNames...), "ipAddresses", api.StringList(ipAddresses...)))
}

// coveredServer listens on HTTPS at the host's IP and name and on HTTP at its
// other IP, under a wildcard bind with an endpoint for every listener.
func coveredServer() api.Object {
	return managedService(api.ArtifactServer, m(
		"bindAddress", "0.0.0.0",
		"tls", m("secretRef", "serving"),
		"listeners", list(
			m("name", "https", "protocol", "https", "port", api.IntegerValue("8443")),
			m("name", "http", "protocol", "http", "port", api.IntegerValue("8080")),
		),
		"endpoints", list(
			m("name", "ip-https", "listenerRef", "https", "addressRef", "ip"),
			m("name", "other-http", "listenerRef", "http", "addressRef", "other"),
			m("name", "name-https", "listenerRef", "https", "addressRef", "fqdn"),
		),
	))
}

// A generated serving certificate names exactly what its declaration lists,
// so admission refuses one that misses an address an HTTPS endpoint answers
// on, at that endpoint, naming the Secret, the address and the field to add it
// to, instead of the apply refusing after its operation registered. A name is
// compared without case and an IPv4-mapped IPv6 address names its IPv4 one,
// as hostname verification compares them; an HTTP endpoint needs no
// certificate, and a contextStore certificate's material is not admission's to
// read.
func TestAGeneratedServingCertificateCoversEveryHTTPSEndpoint(t *testing.T) {
	host := consumerHost(true, consumerAddresses()...)
	covered := servingSecret(generatedSource([]string{"HOST.Example.Test"}, []string{"192.0.2.10"}))
	if issues := admit(coveredServer(), host, covered); len(issues) != 0 {
		t.Fatalf("a certificate naming every HTTPS address was refused: %v", issues)
	}
	mapped := servingSecret(generatedSource([]string{"host.example.test"}, []string{"::ffff:192.0.2.10"}))
	if issues := admit(coveredServer(), host, mapped); len(issues) != 0 {
		t.Fatalf("an IPv4-mapped IPv6 address did not name its IPv4 address: %v", issues)
	}
	stored := servingSecret(m("contextStore", m()))
	if issues := admit(coveredServer(), host, stored); len(issues) != 0 {
		t.Fatalf("a contextStore certificate was judged at admission: %v", issues)
	}
	uncovered := servingSecret(generatedSource([]string{"host.example.test"}, []string{"192.0.2.99"}))
	issues := admit(coveredServer(), host, uncovered)
	found := refusalsAt(issues, "$.spec.endpoints[0]")
	if len(issues) != 1 || len(found) != 1 || found[0].Code != "api.invariant" ||
		found[0].Message != "endpoint ip-https serves HTTPS at 192.0.2.10, which the generated certificate of Secret/serving does not name" ||
		found[0].Remediation != "add 192.0.2.10 to spec.source.generated.ipAddresses of Secret/serving, import the change with bootwright context update --name <context> --input-dir <dir>, "+
			"then run bootwright secret generate --name serving --context <context>" {
		t.Fatalf("an uncovered HTTPS address = %#v, want one refusal at the endpoint naming the Secret and the address", issues)
	}
	unnamed := servingSecret(generatedSource([]string{"other.example.test"}, []string{"192.0.2.10"}))
	issues = admit(coveredServer(), host, unnamed)
	found = refusalsAt(issues, "$.spec.endpoints[2]")
	if len(issues) != 1 || len(found) != 1 || !mentionsAll(found[0].Message, "name-https", "host.example.test", "Secret/serving") ||
		!mentionsAll(found[0].Remediation, "host.example.test", "spec.source.generated.dnsNames") {
		t.Fatalf("an uncovered HTTPS name = %#v, want one refusal naming its dnsNames remedy", issues)
	}
}

// The TLS section states admission's coverage remedy with the flag each command
// it names selects the context by, as the refusal gives it: context update
// takes --name, not --context, which only secret generate takes.
func TestTheSpecStatesTheCoverageRemedyWithEachCommandsContextFlag(t *testing.T) {
	uncovered := servingSecret(generatedSource([]string{"host.example.test"}, []string{"192.0.2.99"}))
	found := refusalsAt(admit(coveredServer(), consumerHost(true, consumerAddresses()...), uncovered), "$.spec.endpoints[0]")
	if len(found) != 1 || !mentionsAll(found[0].Remediation, "bootwright context update --name <context> ", "bootwright secret generate --name serving --context <context>") {
		t.Fatalf("the coverage refusal = %#v, want a remedy naming context update --name and secret generate --context", found)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "specs", "infrastructure-services.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, sentence, ok := strings.Cut(strings.Join(strings.Fields(string(data)), " "), "naming the Secret and the missing address, with the remedy to ")
	sentence, _, _ = strings.Cut(sentence, " since generation reads")
	if !ok || !strings.Contains(sentence, "import the change with `context update --name <context>` and then run `secret generate` with `--context`") ||
		strings.Contains(sentence, "each with `--context`") {
		t.Fatalf("specs/infrastructure-services.md states the coverage remedy as %q, want context update with --name and secret generate with --context", sentence)
	}
}
