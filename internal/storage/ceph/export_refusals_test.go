package ceph

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestConsumerRefusalsNameTheMissingPrerequisite(t *testing.T) {
	block := managedExportPlan(t, false)
	catalog := filesystemCatalog(withMDS)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	base, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	base.Action, base.Controller = "cluster", "controller"
	for i := range base.Hosts {
		base.Hosts[i].HostKeyRef = base.Hosts[i].Name + "-host-key"
	}
	request, err := DecodeConsumerRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	request.HostSecretRefs = hostSecretRefs(base.Hosts)
	if block.Request, err = request.Canonical(); err != nil {
		t.Fatal(err)
	}
	e := lifecycle.Execution{Block: reconciliation.Block{BlockDefinition: block}, ApplyOperation: "apply-original", Operation: "apply-retry", ApplyProved: []lifecycle.BlockEvidence{coreClusterProof(t, base, nil), imageProof(t, "example", base.Image, resolvedDigest)}}
	e = withMaterial(e, base)
	for token, want := range map[string]string{
		"export-prerequisite-pool-identity": "pool", "export-prerequisite-rbd-application": "RBD application", "export-prerequisite-filesystem": "filesystem", "export-prerequisite-csi-group": "csi", "export-prerequisite-prometheus": "Prometheus", "export-prerequisite-monitor-leader": "MON leader", "export-prerequisite-monitor-v2": "v2", "export-prerequisite-monitoring-address": "monitoring", "export-prerequisite-realm": "realm", "export-prerequisite-zonegroup": "zone group", "export-prerequisite-zone": "zone", "export-prerequisite-gateway-query": "radosgw-admin", "export-key-type": "allowed", "export-key-type-aes": "aes", "export-key-type-aes256k": "aes256k",
	} {
		t.Run(token, func(t *testing.T) {
			runner := &refusingRunner{reason: token}
			_, _, err := NewConsumer(runner).WithProofs(hostProofs{}).runExport(context.Background(), e, "observe")
			issues := diagnostics.Of(err)
			if runner.request.Implementation != ConsumerImplementation || len(issues) != 1 || issues[0].Code != "lifecycle.unknown" || !strings.Contains(issues[0].Message, want) || issues[0].Remediation == "" || strings.Contains(issues[0].Message, "Ceph consumer refused:") {
				t.Fatalf("%s: %+v", token, issues)
			}
		})
	}
}

func TestConsumerMapsEveryNativeExportRefusal(t *testing.T) {
	source := string(ansible.Assets()["collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_export_native.py"])
	_, tuple, found := strings.Cut(source, "\nREASONS = (")
	tuple, _, closed := strings.Cut(tuple, ")\n")
	if !found || !closed {
		t.Fatal("native export refusal vocabulary missing")
	}
	named := consumerRefusals()
	for _, match := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(tuple, -1) {
		reason := match[1]
		if reason == "gateway-dns" {
			continue
		}
		if named[reason] == nil {
			t.Errorf("native export refusal %s has no diagnostic", reason)
		}
	}
}
