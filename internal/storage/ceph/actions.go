package ceph

import (
	"regexp"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type actionRow struct {
	arm       func(Request) bool
	object    func(Request) string
	resource  func(Evidence, Request) bool
	scope     func(Evidence, Request) bool
	ready     func(Evidence, Request) bool
	deadline  func(Request, string) (time.Duration, error)
	scopeKind api.Kind
	scopeText func(Request) string
}

var actions = map[string]actionRow{
	"cluster": {scope: bareScope, ready: stretchReady},
	"settings": {arm: func(r Request) bool { return r.Settings != nil }, scope: bareScope,
		ready: func(_ Evidence, r Request) bool { return r.Settings != nil }},
	"service": {arm: func(r Request) bool { return r.Service != nil }, scope: bareScope, ready: serviceReady, deadline: unitDeadline},
	"policy": numbered(func(r Request) bool { return r.Policy != nil }, func(r Request) string { return r.Policy.Name }, resourceID,
		api.CephPlacementPolicy, policyScopeText),
	"pool":       numbered(func(r Request) bool { return r.Pool != nil }, poolObject, resourceID, api.CephPool, poolScopeText),
	"rbd":        numbered(func(r Request) bool { return r.RBD != nil }, func(r Request) string { return r.RBD.Name }, rbdImageID, api.CephRBDImage, rbdScopeText),
	"filesystem": numbered(func(r Request) bool { return r.Filesystem != nil }, func(r Request) string { return r.Filesystem.Name }, resourceID, api.CephFilesystem, filesystemScopeText),
	"client": {arm: func(r Request) bool { return r.Client != nil }, object: func(r Request) string { return r.Client.Name }, resource: clientResource,
		scope: clientScope, ready: armed(func(r Request) bool { return r.Client != nil }, clientResource), scopeKind: api.CephClient, scopeText: clientScopeText},
	"subvolume": located(func(r Request) bool { return r.Subvolume != nil }, func(r Request) string { return r.Subvolume.Name }, subvolumeResource, nil,
		api.CephSubvolume, subvolumeScopeText),
	"nvmeof": located(func(r Request) bool { return r.NVMeOF != nil }, func(r Request) string { return r.NVMeOF.Name }, nvmeofResource, nvmeofDeadline,
		api.CephNVMeoFSubsystem, nvmeofScopeText),
	"nfs-export": located(func(r Request) bool { return r.NFSExport != nil }, func(r Request) string { return r.NFSExport.Name }, nfsExportResource,
		fixedDeadline(nfsExportDeadline), api.CephNFSExport, nfsExportScopeText),
	"object-user": located(func(r Request) bool { return r.ObjectUser != nil }, func(r Request) string { return r.ObjectUser.Name }, objectUserResource,
		fixedDeadline(objectUserDeadline), api.CephObjectUser, objectUserScopeText),
	"object-bucket": located(func(r Request) bool { return r.ObjectBucket != nil }, func(r Request) string { return r.ObjectBucket.Name }, objectBucketResource,
		fixedDeadline(objectBucketDeadline), api.CephObjectBucket, objectBucketScopeText),
	"gateway": {arm: func(r Request) bool { return r.Gateway != nil }, resource: gatewayResource,
		scope: ownScope(func(r Request) bool { return r.Gateway != nil }, gatewayResource), ready: gatewayReady,
		deadline: gatewayDeadline, scopeKind: api.CephObjectGateway, scopeText: gatewayScopeText},
}

func numbered(arm func(Request) bool, object func(Request) string, id *regexp.Regexp, kind api.Kind, text func(Request) string) actionRow {
	resource := func(e Evidence, _ Request) bool { return id.MatchString(e.Resource.ID) }
	scope := func(e Evidence, _ Request) bool {
		return e.Resource.Path == "" && e.Client == "" && (e.Resource.ID == "" || id.MatchString(e.Resource.ID))
	}
	return actionRow{arm: arm, object: object, resource: resource, scope: scope, ready: armed(arm, resource), scopeKind: kind, scopeText: text}
}

func located(arm func(Request) bool, object func(Request) string, resource func(Evidence, Request) bool, deadline func(Request, string) (time.Duration, error),
	kind api.Kind, text func(Request) string) actionRow {
	return actionRow{arm: arm, object: object, resource: resource, scope: ownScope(arm, resource), ready: armed(arm, resource), deadline: deadline, scopeKind: kind, scopeText: text}
}

func armed(arm func(Request) bool, resource func(Evidence, Request) bool) func(Evidence, Request) bool {
	return func(e Evidence, r Request) bool { return arm(r) && resource(e, r) }
}

func ownScope(arm func(Request) bool, resource func(Evidence, Request) bool) func(Evidence, Request) bool {
	return func(e Evidence, r Request) bool {
		return e.Client == "" && arm(r) && (e.Resource == ResourceEvidence{} || resource(e, r))
	}
}

func fixedDeadline(deadline func(string) time.Duration) func(Request, string) (time.Duration, error) {
	return func(_ Request, operation string) (time.Duration, error) { return deadline(operation), nil }
}

func bareScope(e Evidence, _ Request) bool {
	return e.Client == "" && e.Resource == ResourceEvidence{}
}

func clientScope(e Evidence, r Request) bool {
	return e.Resource.Path == "" && r.Client != nil && (e.Client == "" || clientResource(e, r)) && e.Resource == (ResourceEvidence{})
}

func clientResource(e Evidence, r Request) bool { return e.Client == "client."+r.Client.Name }

func poolObject(r Request) string {
	if r.Pool.Object != "" {
		return r.Pool.Object
	}
	return r.Pool.Name
}

func gatewayResource(e Evidence, r Request) bool {
	return zoneID.MatchString(e.Resource.ID) && e.Resource.Path == r.Gateway.Endpoint
}

func nfsExportResource(e Evidence, r Request) bool {
	return exportID.MatchString(e.Resource.ID) && e.Resource.Path == r.NFSExport.Native.Pseudo
}

func objectUserResource(e Evidence, r Request) bool {
	return e.Resource == ResourceEvidence{ID: r.ObjectUser.Name}
}

func objectBucketResource(e Evidence, r Request) bool {
	return e.Resource == ResourceEvidence{ID: r.ObjectBucket.Name}
}

func serviceReady(e Evidence, r Request) bool {
	return r.Service != nil && everyDocumentRuns(e, r) && (r.Service.Type != "mgmt-gateway" || e.Endpoint.Reachable) &&
		(r.Service.Type != SignOnUnitType || e.SSO == "oauth2" && e.Endpoint.Reachable)
}

func gatewayReady(e Evidence, r Request) bool {
	return r.Gateway != nil && gatewayResource(e, r) && everyDocumentRuns(e, r) && e.Endpoint.Reachable
}
