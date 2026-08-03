package desiredstate

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

const siteEnvironmentYAML = `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  domains:
    base: bootwright.test
  sites:
    - name: dc1
    - name: dc3
      description: arbiter-only site
`

const siteMachineYAML = `apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: ceph-0
spec:
  capabilities:
    - ceph-node
  placement:
    site: SITE
  os:
    provided: true
  addresses:
    - name: ssh
      address: 192.0.2.10
  access:
    ssh:
      user: root
      auth:
        privateKeyRef: ceph-node-ssh
      addressRef: ssh
`

const siteStorageYAML = `apiVersion: bootwright.io/v1alpha1
kind: StorageCluster
metadata:
  name: ceph
spec:
  type: ceph
  ceph:
    cephadm:
      clusterSSH:
        user: root
      bootstrap:
        node: node01
    topology:
      nodes:
        - name: node01
          machineRef: ceph-0
          roles: [mon, mgr]
`

func loadSiteFixture(t *testing.T, environment, machine, storage string) (v1alpha1.State, error) {
	t.Helper()
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"environment.yaml": environment,
		"secrets.yaml":     cephNodeSSHSecretYAML,
		"machine.yaml":     machine,
		"storage.yaml":     storage,
	})
	return LoadNormalizeValidate([]string{dir})
}

func TestMachineSiteMustNameADeclaredSite(t *testing.T) {
	machine := strings.Replace(siteMachineYAML, "site: SITE", "site: dc9", 1)
	_, err := loadSiteFixture(t, siteEnvironmentYAML, machine, siteStorageYAML)
	if err == nil {
		t.Fatal("a machine standing in an undeclared site must be refused; the registry is what turns a typo into an error instead of an extra CRUSH bucket")
	}
	if !strings.Contains(err.Error(), "dc9") || !strings.Contains(err.Error(), "spec.sites") {
		t.Fatalf("refusal = %v, want it to name the site and the registry that does not declare it", err)
	}
}

func TestSiteReferenceRequiresARegistry(t *testing.T) {
	environment := `apiVersion: bootwright.io/v1alpha1
kind: Environment
metadata:
  name: env
spec:
  domains:
    base: bootwright.test
`
	machine := strings.Replace(siteMachineYAML, "site: SITE", "site: dc1", 1)
	_, err := loadSiteFixture(t, environment, machine, siteStorageYAML)
	if err == nil || !strings.Contains(err.Error(), "spec.sites") {
		t.Fatalf("naming a site with no registry = %v, want a refusal pointing at Environment spec.sites", err)
	}
}

func TestStorageNodeSiteIsDerivedFromItsMachine(t *testing.T) {
	machine := strings.Replace(siteMachineYAML, "site: SITE", "site: dc1", 1)
	state, err := loadSiteFixture(t, siteEnvironmentYAML, machine, siteStorageYAML)
	if err != nil {
		t.Fatalf("LoadNormalizeValidate: %v", err)
	}
	node := state.StorageClusters[0].Spec.Ceph.Topology.Nodes[0]
	if node.Site != "dc1" {
		t.Fatalf("node site = %q, want dc1 taken from the bound machine; the mon crush_location renders from it", node.Site)
	}
}

func TestStorageNodeSiteMustAgreeWithItsMachine(t *testing.T) {
	machine := strings.Replace(siteMachineYAML, "site: SITE", "site: dc1", 1)
	storage := strings.Replace(siteStorageYAML, "          machineRef: ceph-0\n", "          machineRef: ceph-0\n          site: dc3\n", 1)
	_, err := loadSiteFixture(t, siteEnvironmentYAML, machine, storage)
	if err == nil {
		t.Fatal("a node claiming a site its machine does not stand in must be refused; the cluster would tell Ceph a location the host is not in")
	}
	if !strings.Contains(err.Error(), "does not match Machine/ceph-0") {
		t.Fatalf("refusal = %v, want it to name the machine it disagrees with", err)
	}
}

func TestDuplicateSiteNamesAreRefused(t *testing.T) {
	environment := strings.Replace(siteEnvironmentYAML, "    - name: dc3\n      description: arbiter-only site\n", "    - name: dc1\n", 1)
	machine := strings.Replace(siteMachineYAML, "site: SITE", "site: dc1", 1)
	_, err := loadSiteFixture(t, environment, machine, siteStorageYAML)
	if err == nil || !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("a duplicated site name = %v, want a refusal", err)
	}
}
