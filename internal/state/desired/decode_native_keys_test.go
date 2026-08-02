package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

func decodeContainerClusterDoc(t *testing.T, spec string) error {
	t.Helper()
	doc := "apiVersion: bootwright.io/v1alpha1\nkind: ContainerCluster\nmetadata:\n  name: ocp\nspec:\n" + spec
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var cluster v1alpha1.ContainerCluster
	return decodeKnown(node, &cluster)
}

func TestNativeKeyRedirectsNameTheOwningPath(t *testing.T) {
	cases := []struct {
		name string
		spec string
		want string
	}{
		{
			name: "baseDomain",
			spec: "  baseDomain: example.com\n",
			want: `field "baseDomain" is not authored here; Environment spec.domains.containerClusters owns the fleet DNS zone every container cluster installs under`,
		},
		{
			name: "controlPlane",
			spec: "  controlPlane:\n    replicas: 3\n",
			want: `field "controlPlane" is not authored here; control-plane and compute replica counts are derived from spec.nodes[].role`,
		},
		{
			name: "compute",
			spec: "  compute:\n    - replicas: 2\n",
			want: `field "compute" is not authored here; control-plane and compute replica counts are derived from spec.nodes[].role`,
		},
		{
			name: "sshKey",
			spec: "  sshKey: ssh-ed25519 AAAA\n",
			want: `field "sshKey" is not authored here; spec.install.nodeSSH.keyPairRef names the sshKeyPair Secret; desired state carries no key material`,
		},
		{
			name: "pullSecret",
			spec: "  pullSecret: '{}'\n",
			want: `field "pullSecret" is not authored here; spec.install.pullSecretRef names the pull-secret Secret; desired state carries no secret bytes`,
		},
		{
			name: "additionalTrustBundle",
			spec: "  additionalTrustBundle: |\n    -----BEGIN CERTIFICATE-----\n",
			want: `field "additionalTrustBundle" is not authored here; spec.install.additionalTrustBundleRefs[] names the trust-bundle Secrets; desired state carries no certificate bytes`,
		},
		{
			name: "apiVIPs",
			spec: "  install:\n    platform:\n      baremetal:\n        apiVIPs:\n          - 192.168.140.5\n",
			want: `field "apiVIPs" is not authored here; spec.install.endpoints.api owns the API VIP: set address, source.type: infraComponent, or source.type: node on a single-node cluster. The slot carries one address because single-stack is the current scope, so there is no per-family list to author`,
		},
		{
			name: "ingressVIPs",
			spec: "  install:\n    platform:\n      baremetal:\n        ingressVIPs:\n          - 192.168.140.6\n",
			want: `field "ingressVIPs" is not authored here; spec.install.endpoints.ingress owns the ingress VIP: set address, source.type: infraComponent, or source.type: node on a single-node cluster. The slot carries one address because single-stack is the current scope, so there is no per-family list to author`,
		},
		{
			name: "machineNetwork",
			spec: "  networking:\n    machineNetwork:\n      - cidr: 192.168.140.0/24\n",
			want: `field "machineNetwork" is not authored here; NetworkConfig spec.machineNetwork[] owns the machine networks, and each node reaches it through spec.nodes[].machineRef`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := decodeContainerClusterDoc(t, tc.spec)
			if err == nil {
				t.Fatalf("expected a strict-decode rejection for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %q, want it to contain %q", err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "did you mean") {
				t.Errorf("a redirected native key must not fall through to the did-you-mean path: %q", err.Error())
			}
		})
	}
}

func TestNativeKeyRedirectLeavesTypoSuggestionsAlone(t *testing.T) {
	err := decodeContainerClusterDoc(t, "  networkings:\n    networkType: OVNKubernetes\n")
	if err == nil {
		t.Fatal("expected a strict-decode rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "field networkings not found") {
		t.Errorf("reject wording changed, breaking the pinned contract: %q", msg)
	}
	if !strings.Contains(msg, `did you mean "networking"?`) {
		t.Errorf("expected a did-you-mean suggestion, got %q", msg)
	}
	if strings.Contains(msg, "is not authored here") {
		t.Errorf("a typo must not be reported as a relocated native key: %q", msg)
	}
}

func TestNativeKeyRedirectOnlyAppliesToItsOwnKind(t *testing.T) {
	doc := "apiVersion: bootwright.io/v1alpha1\nkind: Environment\nmetadata:\n  name: env\nspec:\n  baseDomain: example.com\n"
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &node); err != nil {
		t.Fatalf("unmarshal node: %v", err)
	}
	var env v1alpha1.Environment
	err := decodeKnown(node, &env)
	if err == nil {
		t.Fatal("expected a strict-decode rejection")
	}
	if strings.Contains(err.Error(), "is not authored here") {
		t.Errorf("the ContainerCluster redirect table must not fire on Environment: %q", err.Error())
	}
}

func TestProvisioningNetworkRejectionNamesTheNativeSpellings(t *testing.T) {
	errs := validateClusterPlatform("ContainerCluster/ocp spec.install.platform", v1alpha1.InstallPlatform{
		Type:      v1alpha1.PlatformTypeBareMetal,
		BareMetal: &v1alpha1.BareMetalInstallPlatform{ProvisioningNetwork: "Disabled"},
	}, true)
	if !containsSubstring(errs, "must be one of {disabled, managed, unmanaged}") {
		t.Fatalf("expected the accepted value set, got %v", errs)
	}
	if !containsSubstring(errs, "native install-config values Disabled/Managed/Unmanaged in Bootwright's lowercase house style") {
		t.Fatalf("expected the native-spelling redirect, got %v", errs)
	}
}
