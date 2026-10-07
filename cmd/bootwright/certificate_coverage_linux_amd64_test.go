//go:build linux && amd64

package main

import "testing"

// Every example's generated serving certificate names each address its HTTPS
// endpoints answer on, and one that misses that address refuses at validate,
// before any plan registers an operation, naming the Secret and the address.
// Generation compiles the context's stored revision, so the remedy imports the
// change before it generates, and names the context as every remedy does.
func TestAGeneratedServingCertificateMissingAnHTTPSAddressRefusesBeforePlan(t *testing.T) {
	for _, example := range []string{"lab-rhel", "lab-sno", "lab-baremetal", "multidc-platform"} {
		t.Run(example, func(t *testing.T) {
			compileAcceptance(t, exampleDirectory(t, example))
		})
	}
	edit := exampleEdit{"secret-descriptors/artifact-server-tls.yaml", "ipAddresses:\n        - 192.0.2.1\n", "ipAddresses:\n        - 192.0.2.99\n"}
	refusal := requireRefusal(t, editedExample(t, "lab-baremetal", edit), "api.invariant", "ArtifactServer/lab-artifacts", "$.spec.endpoints[0]")
	if refusal.Message != "endpoint ip-https serves HTTPS at 192.0.2.1, which the generated certificate of Secret/artifact-server-tls does not name" ||
		refusal.Remediation != "add 192.0.2.1 to spec.source.generated.ipAddresses of Secret/artifact-server-tls, import the change with bootwright context update --name <context> --input-dir <dir>, "+
			"then run bootwright secret generate --name artifact-server-tls --context <context>" {
		t.Fatalf("refusal = %#v", refusal)
	}
}
