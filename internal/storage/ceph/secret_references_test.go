package ceph

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
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
// bucket bindings, package sources and SMB users. A consumer, an SMB resource
// and a replication name every host key their plan binds; physical retirement
// names none.
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
		"export":                            {NewConsumer(nil).SecretReferences, reconciliation.Block{BlockDefinition: export}, []string{"fleet-key"}},
		"export with a gateway certificate": {NewConsumer(nil).SecretReferences, frozenCeph(t, securedExport), []string{"fleet-key", "gateway-tls"}},
		"SMB users":                         {NewSMB(nil).SecretReferences, frozenCeph(t, builtSMB(t, catalog, base, "smb-users")), []string{"alice-password", "bob-password", "fleet-key"}},
		"SMB share":                         {NewSMB(nil).SecretReferences, frozenCeph(t, builtSMB(t, catalog, base, "smb-docs")), []string{"fleet-key"}},
		"replication":                       {NewReplication(nil).SecretReferences, replication, []string{"fleet-key"}},
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

func TestAConsumerBindingAloneHoldsTheHostKeysItPins(t *testing.T) {
	r, err := DecodeConsumerRequest(managedExportPlan(t, true).Request)
	if err != nil {
		t.Fatal(err)
	}
	r.HostSecretRefs = []string{"fleet-key", "node-1-host-key", "tie-known-hosts"}
	r.Gateway.TLS, r.Gateway.CertificateRef = true, "gateway-tls"
	held, classified := lifecycle.EffectMaterial(NewConsumer(nil), frozenCeph(t, r))
	if want := []string{"fleet-key", "gateway-tls", "node-1-host-key", "tie-known-hosts"}; !classified || !slices.Equal(held, want) {
		t.Fatalf("a consumer block alone holds %v (classified %v), want %v", held, classified, want)
	}
}

func TestAReplicationBindingAloneHoldsBothEndpointsHostKeys(t *testing.T) {
	r, _ := replicationPlan(t, replicationCatalog("image"))
	r.Primary.HostSecretRefs = []string{"fleet-key", "node-1-host-key"}
	r.Secondary.HostSecretRefs = []string{"peer-key", "tie-known-hosts"}
	held, classified := lifecycle.EffectMaterial(NewReplication(nil), frozenCeph(t, r))
	if want := []string{"fleet-key", "node-1-host-key", "peer-key", "tie-known-hosts"}; !classified || !slices.Equal(held, want) {
		t.Fatalf("a replication block alone holds %v (classified %v), want %v", held, classified, want)
	}
}

func TestAPlannedConsumerOrReplicationHoldsEverySecretItNames(t *testing.T) {
	_, replication := replicationPlan(t, replicationCatalog("image"))
	export := managedExportPlan(t, false)
	for name, tc := range map[string]struct {
		capability lifecycle.Capability
		block      reconciliation.Block
	}{
		"consumer":    {NewConsumer(nil), reconciliation.Block{BlockDefinition: export}},
		"replication": {NewReplication(nil), replication},
	} {
		t.Run(name, func(t *testing.T) {
			references, err := tc.capability.(lifecycle.SecretReferencer).SecretReferences(tc.block)
			held, classified := lifecycle.EffectMaterial(tc.capability, tc.block)
			if err != nil || !classified || len(references) == 0 || !slices.Equal(held, references) {
				t.Fatalf("references %v (%v), held %v (classified %v)", references, err, held, classified)
			}
		})
	}
}

func TestAnSMBBindingAloneHoldsTheHostKeysItPins(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs())
	r := builtSMB(t, catalog, sambaBase(t, catalog), "smb-docs")
	r.HostSecretRefs = []string{"fleet-key", "node-1-host-key", "tie-known-hosts"}
	held, classified := lifecycle.EffectMaterial(NewSMB(nil), frozenCeph(t, r))
	if !classified || !slices.Equal(held, r.HostSecretRefs) {
		t.Fatalf("an SMB block alone holds %v (classified %v), want %v", held, classified, r.HostSecretRefs)
	}
}
