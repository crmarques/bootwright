package ceph

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
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

func ObjectUserBlockID(name string) string { return "ceph-object-user-" + name }

func objectUserRequest(c api.Catalog, user api.Object, contextName string) (Request, error) {
	name, spec := user.Name(), user.Spec()
	if !reconciliation.ValidSegment(ObjectUserBlockID(name)) {
		return Request{}, failure("api.invariant", "StorageObjectUser "+name+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageObjectUser to at most 46 characters")
	}
	gateway, _ := c.Find(api.StorageObjectGateway, spec.Get("objectGatewayRef").Text())
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
