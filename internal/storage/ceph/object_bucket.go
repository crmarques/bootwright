package ceph

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/storage"
)

const maxBucketPolicy = 65536

type ObjectBucket struct {
	Backend        string `json:"backend"`
	CertificateRef string `json:"certificateRef"`
	Endpoint       string `json:"endpoint"`
	Gateway        string `json:"gateway"`
	Name           string `json:"name"`
	Owner          string `json:"owner"`
	Policy         string `json:"policy"`
	Port           int64  `json:"port"`
	Realm          string `json:"realm"`
	Zone           string `json:"zone"`
	ZoneGroup      string `json:"zoneGroup"`
}

func ObjectBucketBlockID(name string) string { return "ceph-object-bucket-" + name }

func objectBucketRequest(c api.Catalog, bucket api.Object, contextName string) (Request, error) {
	name, spec := bucket.Name(), bucket.Spec()
	if !reconciliation.ValidSegment(ObjectBucketBlockID(name)) {
		return Request{}, failure("api.invariant", "StorageObjectBucket "+name+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageObjectBucket to at most 44 characters")
	}
	gateway, _ := c.Find(api.StorageObjectGateway, spec.Get("objectGatewayRef").Text())
	requests, err := gatewayRequests(c, gateway, contextName)
	if err != nil {
		return Request{}, err
	}
	r := requests[len(requests)-1]
	g := r.Gateway
	policy, err := bucketPolicy(bucket)
	if err != nil {
		return Request{}, err
	}
	cluster, _ := c.Find(api.StorageCluster, gateway.Spec().Get("clusterRef").Text())
	backend, _ := storage.NativeService(cluster, gateway.Spec().Get("serviceRef").Text())
	frozen := ObjectBucket{Backend: storage.NativeServiceName(backend), Gateway: g.Name, Name: name, Owner: spec.Get("ownerRef").Text(), Policy: policy, Port: 80,
		Realm: g.Realm, Zone: g.Zone, ZoneGroup: g.ZoneGroup}
	if backend.Get("spec", "ssl").Bool() {
		frozen.Port, frozen.Endpoint = 443, g.Endpoint
		for _, binding := range g.Bindings {
			if binding.ServiceRef == frozen.Backend {
				frozen.CertificateRef = binding.SecretRef
			}
		}
	}
	if port, ok := backend.Get("spec", "rgw_frontend_port").Int64(); ok {
		frozen.Port = port
	}
	r.Action, r.Gateway, r.Pool = "object-bucket", nil, nil
	r.ObjectBucket = &frozen
	return r, nil
}

func bucketPolicy(bucket api.Object) (string, error) {
	declared := bucket.Spec().Get("policy")
	if !declared.Present() {
		return "", nil
	}
	name := bucket.Name()
	value, ok := smbValue(declared)
	if _, mapped := value.(map[string]any); !ok || !mapped {
		return "", failure("api.invariant", "StorageObjectBucket "+name+" declares a policy holding a value that is not a JSON string, integer, boolean, list or mapping", "write spec.policy as the native S3 bucket policy document with only strings, integers, booleans, lists and mappings")
	}
	encoded, err := canonicaljson.Encode(value, canonicaljson.Bare)
	if err != nil || len(encoded) > maxBucketPolicy {
		return "", failure("api.invariant", "StorageObjectBucket "+name+" declares a policy larger than 65536 bytes of JSON", "shorten spec.policy to at most 65536 bytes of JSON")
	}
	return string(encoded), nil
}

func objectBucketDeadline(operation string) time.Duration {
	budget := map[string]unitBudget{"apply": {calls: 2, waits: 1}, "observe": {calls: 2}, "destroy": {calls: 2}}[operation]
	return time.Duration(budget.calls*nativeCall+budget.waits*readinessWait+60) * time.Second
}
