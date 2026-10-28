//go:build linux && amd64

package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// Each nested, endpoint and native-service reference an example declares is
// resolved by the shared unresolved-reference diagnostic alone: one that names
// nothing its owner declares is one api.reference at the field, naming the
// owner, and no owner's admission adds a refusal of its own there or at a
// field of the record that holds it. What the edit changes elsewhere, such as
// a service that loses its binding, stays its own diagnostic.
func TestEveryNestedReferenceKindRefusesOnlyThroughTheSharedDiagnostic(t *testing.T) {
	const machine = "infra/machines/rhel-03.yaml"
	const cluster = "storage/cluster.yaml"
	for _, row := range []struct {
		name, example string
		edit          exampleEdit
		want          []string
	}{
		{"a provider machine profile", "lab-compact-df", exampleEdit{machine, "profileRef: rhel", "profileRef: absent"},
			[]string{"Machine/rhel-03 $.spec.substrate.libvirt.profileRef no machine profile named absent is declared on InfraProvider/lab-libvirt"}},
		{"a provider network attachment", "lab-compact-df", exampleEdit{machine, "attachmentRef: lab-guests", "attachmentRef: absent"},
			[]string{"Machine/rhel-03 $.spec.network.attachmentRef no network attachment named absent is declared on InfraProvider/lab-libvirt"}},
		{"a Machine install address", "lab-compact-df", exampleEdit{machine, "installAddressRef: ip", "installAddressRef: absent"},
			[]string{"Machine/rhel-03 $.spec.network.installAddressRef no address named absent is declared on Machine/rhel-03"}},
		{"a server endpoint selection", "lab-compact-df", exampleEdit{machine, "endpointRef: ip", "endpointRef: absent"},
			[]string{"Machine/rhel-03 $.spec.os.install.ntp[0].endpointRef no endpoint named absent is declared on NTPServer/lab-ntp"}},
		{"an artifact endpoint selection", "lab-compact-df", exampleEdit{"infra/os/rhel-9-8.yaml", "endpointRef: ip-https", "endpointRef: absent"},
			[]string{"MachineInstallProfile/rhel-9-8 $.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.endpointRef no endpoint named absent is declared on ArtifactServer/lab-artifacts"}},
		{"a service endpoint address", "lab-compact-df", exampleEdit{"infra/components/ntp.yaml", "addressRef: ip", "addressRef: absent"},
			[]string{"NTPServer/lab-ntp $.spec.endpoints[0].addressRef no address named absent is declared on Machine/controller"}},
		{"an artifact endpoint listener", "lab-compact-df", exampleEdit{"infra/components/artifact-server.yaml", "listenerRef: https", "listenerRef: absent"},
			[]string{"ArtifactServer/lab-artifacts $.spec.endpoints[0].listenerRef no listener named absent is declared on ArtifactServer/lab-artifacts"}},
		{"a bootstrap topology node", "lab-compact-df", exampleEdit{cluster, "nodeRef: rhel-01", "nodeRef: absent"},
			[]string{"CephCluster/ceph-lab $.spec.cephadm.bootstrap.nodeRef no topology node named absent is declared on CephCluster/ceph-lab"}},
		{"a bootstrap node address", "lab-compact-df", exampleEdit{cluster, "nodeRef: rhel-01\n      addressRef: ip", "nodeRef: rhel-01\n      addressRef: absent"},
			[]string{"CephCluster/ceph-lab $.spec.cephadm.bootstrap.addressRef no address named absent is declared on Machine/rhel-01"}},
		{"every node's cephadm address", "lab-compact-df", exampleEdit{cluster, "cephadm:\n    addressRef: ip", "cephadm:\n    addressRef: absent"}, []string{
			"CephCluster/ceph-lab $.spec.cephadm.addressRef no address named absent is declared on Machine/rhel-01",
			"CephCluster/ceph-lab $.spec.cephadm.addressRef no address named absent is declared on Machine/rhel-02",
			"CephCluster/ceph-lab $.spec.cephadm.addressRef no address named absent is declared on Machine/rhel-03"}},
		{"a native service binding", "lab-compact-df", exampleEdit{cluster, "- serviceRef: rgw.objects", "- serviceRef: rgw.absent"},
			[]string{"CephCluster/ceph-lab $.spec.serviceBindings[0].serviceRef no native service named rgw.absent is declared on CephCluster/ceph-lab"}},
		{"a gateway's native service", "lab-compact-df", exampleEdit{"storage/object-gateway.yaml", "serviceRef: rgw.objects", "serviceRef: rgw.absent"},
			[]string{"CephObjectGateway/objects $.spec.serviceRef no native service named rgw.absent is declared on CephCluster/ceph-lab"}},
		{"a bare-metal MAC's hardware NIC", "physical-3-nodes-ceph", exampleEdit{"infra/machines/metal-01.yaml", "- nicRef: eno1", "- nicRef: absent"},
			[]string{"Machine/metal-01 $.spec.substrate.baremetal.nics[0].nicRef no hardware NIC named absent is declared on Machine/metal-01"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, _, err := wireCompiler().Compile(context.Background(), editedExample(t, row.example, row.edit))
			object, field, _ := strings.Cut(row.want[0], " ")
			field, _, _ = strings.Cut(field, " ")
			record := field[:strings.LastIndexByte(field, '.')]
			got := []string{}
			for _, d := range diagnostics.Of(err) {
				if d.Severity != "error" || d.Object == nil || d.Object.Kind+"/"+d.Object.Name != object || !strings.HasPrefix(d.Field, record) {
					continue
				}
				line := object + " " + d.Field + " " + d.Message
				if d.Code != "api.reference" {
					line = d.Code + " " + line
				}
				got = append(got, line)
			}
			slices.Sort(got)
			if !slices.Equal(got, row.want) {
				t.Fatalf("diagnostics:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(row.want, "\n"))
			}
		})
	}
}

const externalTargets = `apiVersion: bootwright.io/v1alpha1
kind: CephCluster
metadata:
  name: ext
spec:
  management: external
---
apiVersion: bootwright.io/v1alpha1
kind: CephObjectGateway
metadata:
  name: extgw
spec:
  clusterRef: ext
  serviceRef: rgw.ext
  realm: a
  zoneGroup: a
  zone: a
---
apiVersion: bootwright.io/v1alpha1
kind: CephFilesystem
metadata:
  name: extfs
spec:
  clusterRef: ext
  metadataPoolRef: cephfs-metadata
  dataPoolRefs: [cephfs-data]
---
apiVersion: bootwright.io/v1alpha1
kind: CephNVMeoFSubsystem
metadata:
  name: extnvme
spec:
  clusterRef: ext
  serviceRef: nvmeof.ext
  nqn: nqn.2026-01.test.example:ext
  listeners: [{host: rhel-01}]
  namespaces: [{nsid: 1, imageRef: rbd-disk}]
  hosts: [nqn.2026-01.test.example:initiator]
---
apiVersion: bootwright.io/v1alpha1
kind: ArtifactServer
metadata:
  name: ext-artifacts
spec:
  management: external
  endpoints: [{name: media, url: "https://artifacts.example.test"}]
`

// A nested reference scoped through an owner its own object refuses at the
// field naming it, such as a child of an external CephCluster or a selection
// of an external ArtifactServer, adds nothing beside that refusal, and a
// reference into entries a required field leaves absent adds nothing beside
// the required diagnostic.
func TestANestedReferenceThroughARefusedOwnerAddsNothing(t *testing.T) {
	sources := extendedExample(t, "lab-compact-df", map[string]string{"storage/external.yaml": externalTargets},
		exampleEdit{"environment.yaml", "  storageClusters:\n    - ceph-lab\n", "  storageClusters:\n    - ceph-lab\n    - ext\n"},
		exampleEdit{"infra/os/rhel-9-8.yaml", "serverRef: lab-artifacts\n          endpointRef: ip-https", "serverRef: ext-artifacts\n          endpointRef: ip-https"})
	_, _, err := wireCompiler().Compile(context.Background(), sources)
	requireOnlyOwnerRefusal(t, diagnostics.Of(err), map[string][2]string{
		"CephObjectGateway/extgw":        {"$.spec.clusterRef", "$.spec.serviceRef"},
		"CephFilesystem/extfs":           {"$.spec.clusterRef", "$.spec.mdsServiceRef"},
		"CephNVMeoFSubsystem/extnvme":    {"$.spec.clusterRef", "$.spec.serviceRef"},
		"MachineInstallProfile/rhel-9-8": {"$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.serverRef", "$.spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint.endpointRef"},
	})
	topology := strings.Join([]string{"  topology:\n    nodes:\n", "      - name: rhel-01\n        fqdn: rhel-01.lab.example.test\n        machineRef: rhel-01\n\n        labels:\n          - ceph-core\n\n",
		"      - name: rhel-02\n        fqdn: rhel-02.lab.example.test\n        machineRef: rhel-02\n\n        labels:\n          - ceph-core\n\n",
		"      - name: rhel-03\n        fqdn: rhel-03.lab.example.test\n        machineRef: rhel-03\n\n        labels:\n          - ceph-core\n\n"}, "")
	_, _, err = wireCompiler().Compile(context.Background(), editedExample(t, "lab-compact-df", exampleEdit{"storage/cluster.yaml", topology, ""}))
	requireOnlyOwnerRefusal(t, diagnostics.Of(err), map[string][2]string{"CephCluster/ceph-lab": {"$.spec.topology", "$.spec.cephadm.bootstrap.nodeRef"}})
}

func requireOnlyOwnerRefusal(t *testing.T, found []diagnostics.Diagnostic, objects map[string][2]string) {
	t.Helper()
	for identity, fields := range objects {
		owner := false
		for _, d := range found {
			if d.Object == nil || d.Object.Kind+"/"+d.Object.Name != identity {
				continue
			}
			owner = owner || d.Field == fields[0]
			if d.Field == fields[1] {
				t.Errorf("%s: %s %s beside its owner's refusal at %s", identity, d.Code, d.Message, fields[0])
			}
		}
		if !owner {
			t.Errorf("%s: no refusal at %s", identity, fields[0])
		}
	}
}
