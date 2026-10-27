package ceph

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const ObjectUserOutput = "object-user"

type ObjectUser struct {
	DisplayName string `json:"displayName"`
	Gateway     string `json:"gateway"`
	Name        string `json:"name"`
	Realm       string `json:"realm"`
	Zone        string `json:"zone"`
	ZoneGroup   string `json:"zoneGroup"`
}

func ObjectUserBlockID(name string) string {
	return reconciliation.BlockIdentity("ceph-object-user-", name)
}

func ObjectUserProved(catalog api.Catalog, user api.Object, proofs []lifecycle.BlockEvidence) bool {
	gateway, exists := catalog.Find(api.CephObjectGateway, user.Spec().Get("objectGatewayRef").Text())
	cluster := user.Spec().Get("clusterRef").Text()
	if !exists || gateway.Spec().Get("clusterRef").Text() != cluster {
		return false
	}
	matched := 0
	for _, proof := range proofs {
		if proof.Kind != string(api.CephObjectUser) || proof.Object != user.Name() || proof.Implementation != Implementation || proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone || proof.Block.ID != ObjectUserBlockID(user.Name()) || proof.Block.Kind != proof.Kind || proof.Block.Object != proof.Object || proof.Block.Implementation != Implementation {
			return false
		}
		r, err := DecodeRequest(proof.Block.Request)
		if err != nil || r.Action != "object-user" || r.Cluster != cluster || r.ObjectUser == nil || r.ObjectUser.Name != user.Name() || r.ObjectUser.Gateway != gateway.Name() || r.ObjectUser.Realm != gateway.Spec().Get("realm").Text() || r.ObjectUser.ZoneGroup != gateway.Spec().Get("zoneGroup").Text() || r.ObjectUser.Zone != gateway.Spec().Get("zone").Text() {
			return false
		}
		e, err := decodeEvidence(proof.Evidence)
		if err != nil || e.Owner == "" {
			return false
		}
		r.Owner, r.FSID = e.Owner, FSIDFor(e.Owner, cluster)
		if effect, err := EvaluateEvidence(proof.Evidence, r, proof.Block.RequestDigest, false); err != nil || effect != reconciliation.EffectCompleted {
			return false
		}
		matched++
	}
	return matched == 1
}

func objectUserRequest(c api.Catalog, user api.Object, contextName string) (Request, error) {
	name, spec := user.Name(), user.Spec()
	gateway, _ := c.Find(api.CephObjectGateway, spec.Get("objectGatewayRef").Text())
	requests, err := gatewayRequests(c, gateway, contextName)
	if err != nil {
		return Request{}, err
	}
	r := requests[len(requests)-1]
	g := r.Gateway
	display := spec.Get("displayName").Text()
	if display == "" {
		display = name
	}
	r.Action, r.Gateway, r.Pool = "object-user", nil, nil
	r.ObjectUser = &ObjectUser{DisplayName: display, Gateway: g.Name, Name: name, Realm: g.Realm, Zone: g.Zone, ZoneGroup: g.ZoneGroup}
	return r, nil
}

func objectUserDeadline(operation string) time.Duration {
	budget := map[string]unitBudget{"apply": {calls: 2, waits: 1}, "observe": {calls: 2}, "destroy": {calls: 2}}[operation]
	return time.Duration(budget.calls*nativeCall+budget.waits*readinessWait+60) * time.Second
}
