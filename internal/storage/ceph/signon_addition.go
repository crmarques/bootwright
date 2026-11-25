package ceph

import (
	"encoding/json"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func signOnSlot(cluster api.Object, hosts []string) []DelegatedService {
	gateway := slices.ContainsFunc(cluster.Spec().Get("services").Items(), func(svc api.Value) bool { return svc.Get("service_type").Text() == "mgmt-gateway" })
	if !gateway || !cluster.Spec().Get("images").Has(signOnImage) || len(hosts) == 0 {
		return nil
	}
	return []DelegatedService{{Name: SignOnUnitType, Hosts: slices.Sorted(slices.Values(hosts))}}
}

func (SignOnCapability) ReadsMaterialOnlyInApply(block reconciliation.Block) bool {
	_, err := DecodeSignOnRequest(block.Request)
	return err == nil
}

func (SignOnCapability) AdmitsAddedObjects(after api.Catalog, added []string) []string {
	var admitted, providers []string
	for _, identity := range added {
		if kind, name, _ := strings.Cut(identity, "/"); kind == string(api.CephSignOn) {
			object, found := after.Find(api.CephSignOn, name)
			if !found {
				continue
			}
			admitted = append(admitted, identity)
			providers = append(providers, string(api.OIDCProvider)+"/"+object.Spec().Get("oidcProviderRef").Text())
		}
	}
	for _, identity := range added {
		if slices.Contains(providers, identity) {
			admitted = append(admitted, identity)
		}
	}
	return admitted
}

func (SignOnCapability) AdmitAdded(lineage reconciliation.Plan, added reconciliation.Block) error {
	r, err := DecodeSignOnRequest(added.Request)
	if err != nil || added.Kind != string(api.CephSignOn) || added.ID != SignOnBlockID(r.Cluster) {
		return cephRequestInvalid.Fail("the Ceph sign-on "+added.ID+" added to a completed apply froze a request this build cannot read", "")
	}
	held := func(id, kind, object string) (reconciliation.Block, bool) {
		at := slices.IndexFunc(lineage.Blocks, func(b reconciliation.Block) bool {
			return b.ID == id && b.Kind == kind && (object == "" || b.Object == object)
		})
		if at < 0 {
			return reconciliation.Block{}, false
		}
		return lineage.Blocks[at], true
	}
	if _, found := held(ClusterBlockID(r.Cluster), string(api.CephCluster), r.Cluster); !found {
		return cephSignOnUnaddable.Fail("CephSignOn "+r.Object+" cannot be added: the completed apply did not install CephCluster "+r.Cluster, "declare it on a cluster that apply installed, or take the apply back and apply again")
	}
	if _, found := held(ServiceUnitBlockID(r.Cluster, ServiceUnit{Type: "mgmt-gateway"}), string(api.CephCluster), r.Cluster); !found {
		return cephSignOnUnaddable.Fail("CephSignOn "+r.Object+" cannot be added: the completed apply installed no management gateway of CephCluster "+r.Cluster, "declare a mgmt-gateway service; on an installed cluster that is an input change: take back what this context owns with bootwright destroy --context <context>, then apply")
	}
	image, found := held(ImageBlockID(r.Cluster), string(api.CephCluster), r.Cluster)
	frozen := ImageRequest{}
	if found {
		if frozen, err = DecodeImageRequest(image.Request); err != nil {
			return err
		}
	}
	if !slices.ContainsFunc(frozen.Auxiliary, func(a AuxiliaryImage) bool { return a.Name == signOnImage && a.Image == r.Image }) {
		return cephSignOnUnaddable.Fail("CephSignOn "+r.Object+" cannot be added: CephCluster "+r.Cluster+" froze no container_image_oauth2_proxy equal to spec.images.oauth2_proxy in the completed apply",
			"set spec.images.oauth2_proxy of CephCluster "+r.Cluster+" before its first apply; on an installed cluster that is an input change: take back what this context owns with bootwright destroy --context <context>, then apply")
	}
	if slices.ContainsFunc(lineage.Blocks, func(b reconciliation.Block) bool {
		other, err := DecodeSignOnRequest(b.Request)
		return b.ID == added.ID || b.Kind == string(api.CephSignOn) && (err != nil || other.Cluster == r.Cluster)
	}) {
		return cephSignOnUnaddable.Fail("CephSignOn "+r.Object+" cannot be added: the completed apply already holds a sign-on", "keep one CephSignOn per cluster; to change it, take back what this context owns with bootwright destroy --context <context>, then apply")
	}
	return nil
}

func (SignOnCapability) UnresolvedAdded(verb reconciliation.Verb, block reconciliation.Block, _ json.RawMessage, _ []lifecycle.OwnedBlock) (lifecycle.Unresolved, bool) {
	r, err := DecodeSignOnRequest(block.Request)
	if err != nil || verb != reconciliation.Apply {
		return lifecycle.Unresolved{}, false
	}
	return lifecycle.Unresolved{Reason: "Ceph sign-on " + r.Object + " of " + r.Cluster + " did not prove its oauth2-proxy daemons, the dashboard's single sign-on and the gateway's enforcement",
		Remedy: "check the oauth2-proxy daemons with ceph orch ps --daemon_type oauth2-proxy and the identity provider of OIDCProvider " + r.Provider.Name + "; the lineage's one destroy takes the sign-on back with its cluster"}, true
}
