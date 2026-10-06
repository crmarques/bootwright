package machine

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// A host key is the machine's own: one that is also the fleet key, another
// Machine's access key or a cluster's SSH key would let the holder of that
// credential answer as this machine, and a container cluster's node public
// key, which every node authorizes, would let this machine's root log in to
// them. Each collision is named once, on the Machine, naming the other object.
func TestAHostKeyIsNoOtherCredential(t *testing.T) {
	machine, catalog := installedFixture()
	key := machine.Spec().Get("os", "install", "hostKeyRef").Text()
	provided := object(api.Machine, "provided", m("os", m("provided", true), "access", m("ssh", m("auth", m("privateKeyRef", key)))))
	environment := object(api.Environment, "env", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", key)))
	cases := map[string]struct {
		objects  []api.Object
		holder   string
		replaces api.Kind
	}{
		"the fleet key":                          {[]api.Object{environment}, "Environment/env's spec.remoteMachinesAccessKey.keyRef", api.Environment},
		"a Machine's access key":                 {[]api.Object{provided}, "Machine/provided's spec.access.ssh.auth.privateKeyRef", ""},
		"a storage cluster's key":                {[]api.Object{object(api.StorageCluster, "ceph", m("ceph", m("cephadm", m("clusterSSH", m("keyRef", key)))))}, "StorageCluster/ceph's spec.ceph.cephadm.clusterSSH.keyRef", ""},
		"a container cluster's key":              {[]api.Object{object(api.ContainerCluster, "ocp", m("install", m("nodeSSH", m("keyPairRef", key))))}, "ContainerCluster/ocp's spec.install.nodeSSH.keyPairRef", ""},
		"a container cluster's node private key": {[]api.Object{object(api.ContainerCluster, "ocp", m("install", m("nodeSSH", m("privateKeyRef", key))))}, "ContainerCluster/ocp's spec.install.nodeSSH.privateKeyRef", ""},
		"a container cluster's node public key":  {[]api.Object{object(api.ContainerCluster, "ocp", m("install", m("nodeSSH", m("publicKeyRef", key, "privateKeyRef", "cluster-private"))))}, "ContainerCluster/ocp's spec.install.nodeSSH.publicKeyRef", ""},
		"a container cluster naming the key as both split halves": {
			[]api.Object{object(api.ContainerCluster, "ocp", m("install", m("nodeSSH", m("publicKeyRef", key, "privateKeyRef", key))))},
			"ContainerCluster/ocp's spec.install.nodeSSH.publicKeyRef", "",
		},
		"a container cluster naming the key in both fields": {
			[]api.Object{object(api.ContainerCluster, "ocp", m("install", m("nodeSSH", m("keyPairRef", key, "privateKeyRef", key))))},
			"ContainerCluster/ocp's spec.install.nodeSSH.keyPairRef", "",
		},
		"the fleet key beside another installed Machine": {
			[]api.Object{environment, object(api.Machine, "peer", m("os", m("provided", false, "installProfileRef", "rhel"), "access", m("ssh", m("auth", m("privateKeyRef", key)))))},
			"Environment/env's spec.remoteMachinesAccessKey.keyRef", api.Environment,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			objects := []api.Object{}
			for _, existing := range catalog.Objects() {
				if existing.Kind() != test.replaces {
					objects = append(objects, existing)
				}
			}
			issues := Validate(machine, api.NewCatalog(append(objects, test.objects...)))
			if len(issues) != 1 || issues[0].Code != "api.invariant" || issues[0].Field != "$.spec.os.install.hostKeyRef" ||
				!strings.Contains(issues[0].Message, "Secret "+key) || !strings.Contains(issues[0].Message, test.holder) ||
				!strings.Contains(issues[0].Remediation, machine.Identity()) {
				t.Fatalf("issues = %#v, want one api.invariant naming %s", issues, test.holder)
			}
		})
	}
	dedicated := []api.Object{}
	for _, existing := range catalog.Objects() {
		if existing.Kind() != api.Environment {
			dedicated = append(dedicated, existing)
		}
	}
	dedicated = append(dedicated, object(api.Environment, "env", m("domains", m("base", "example.test"), "remoteMachinesAccessKey", m("keyRef", "fleet-key"))))
	if issues := Validate(machine, api.NewCatalog(dedicated)); len(issues) != 0 {
		t.Fatalf("a dedicated host key was refused: %v", issues)
	}
}
