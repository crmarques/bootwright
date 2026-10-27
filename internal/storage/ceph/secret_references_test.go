package ceph

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func frozenCeph(t *testing.T, request interface{ Canonical() ([]byte, error) }) reconciliation.Block {
	t.Helper()
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Request: data}}
}

// Each Ceph capability reports the declarations its own frozen request
// references at any depth: keys, registry login and trust, gateway, service and
// bucket bindings, package sources and SMB users. Replication and physical
// retirement name none, because their hosts' keys come from the proved cluster.
func TestCephBlocksReportTheSecretsTheirRequestsReference(t *testing.T) {
	core := coreRequest()
	bound := core
	bound.Gateway = gatewayRuntimeRequest().Gateway
	bound.Gateway.Bindings = []GatewayBinding{{Path: "/spec/ssl_cert", SecretRef: "gateway-tls", ServiceRef: "rgw.objects"}}
	bound.Service = &ServiceUnit{Type: "ingress", ID: "rgw.objects", Bindings: []ServiceBinding{{Path: "/spec/ssl_cert", SecretRef: "ingress-tls", ServiceRef: "ingress.rgw.objects"}}}
	bound.ObjectBucket = &ObjectBucket{CertificateRef: "bucket-tls"}
	hostCall, _, _ := hostExecution(t, "node-2")
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs())
	base := sambaBase(t, catalog)
	export := managedExportPlan(t, true)
	securedExport, err := DecodeConsumerRequest(export.Request)
	if err != nil {
		t.Fatal(err)
	}
	securedExport.Gateway.TLS, securedExport.Gateway.CertificateRef = true, "gateway-tls"
	_, replication := replicationPlan(t, replicationCatalog("image"))
	_, retirement, _, _ := physicalRetirementFixture(t)
	for name, tc := range map[string]struct {
		references func(reconciliation.Block) ([]string, error)
		block      reconciliation.Block
		want       []string
	}{
		"cluster":                           {New("CephCluster", nil).SecretReferences, frozenCeph(t, core), []string{"cluster-key", "fleet-key", "node-1-host-key", "node-2-host-key", "node-3-host-key", "registry"}},
		"packages":                          {New("CephCluster", nil).SecretReferences, frozenCeph(t, packageRequest(t)), []string{"cluster-key", "fleet-key", "registry", "serving-tls"}},
		"bindings":                          {New("CephCluster", nil).SecretReferences, frozenCeph(t, bound), []string{"bucket-tls", "cluster-key", "fleet-key", "gateway-tls", "ingress-tls", "node-1-host-key", "node-2-host-key", "node-3-host-key", "registry"}},
		"host":                              {NewHost(nil).SecretReferences, hostCall.Block, []string{"cluster-key", "fleet-key", "node-1-host-key", "node-2-host-key", "node-3-host-key", "registry", "serving-tls"}},
		"image":                             {NewImage(nil).SecretReferences, imageBlock(t, "example", PinnedRepository+":"+PinnedTag), []string{"registry"}},
		"export":                            {NewConsumer(nil).SecretReferences, reconciliation.Block{BlockDefinition: export}, nil},
		"export with a gateway certificate": {NewConsumer(nil).SecretReferences, frozenCeph(t, securedExport), []string{"gateway-tls"}},
		"SMB users":                         {NewSMB(nil).SecretReferences, frozenCeph(t, builtSMB(t, catalog, base, "smb-users")), []string{"alice-password", "bob-password"}},
		"SMB share":                         {NewSMB(nil).SecretReferences, frozenCeph(t, builtSMB(t, catalog, base, "smb-docs")), nil},
		"replication":                       {NewReplication(nil).SecretReferences, replication, nil},
		"physical retirement":               {NewPhysicalRetirement().SecretReferences, retirement.Block, nil},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := tc.references(tc.block)
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
