package ceph

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const bucketPolicyText = `{"Statement":[{"Action":["s3:GetObject"],"Effect":"Allow","Principal":{"AWS":["arn:aws:iam:::user/app-reader"]},"Resource":["arn:aws:s3:::app-data/*"],"Sid":"ReadOnly"}],"Version":"2012-10-17"}`

func declaredPolicy() api.Value {
	statement := requestMap("Sid", "ReadOnly", "Effect", "Allow", "Action", api.ListValue(api.StringValue("s3:GetObject")),
		"Principal", requestMap("AWS", api.ListValue(api.StringValue("arn:aws:iam:::user/app-reader"))), "Resource", api.ListValue(api.StringValue("arn:aws:s3:::app-data/*")))
	return requestMap("Version", "2012-10-17", "Statement", api.ListValue(statement))
}

func objectBucketObject(name string, pairs ...any) api.Object {
	return childOf(api.StorageObjectBucket, name, append([]any{"objectGatewayRef", "objects", "ownerRef", "app-writer"}, pairs...)...)
}

func bucketCatalog(change func(api.Value) api.Value, gateway, bucket api.Object) api.Catalog {
	return planCatalog(3, 3, change, gateway, objectUserObject("app-writer"), bucket)
}

func plannedBucketBlock(t *testing.T, catalog api.Catalog) (reconciliation.BlockDefinition, []string) {
	t.Helper()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := New(api.StorageObjectBucket, nil).Plan(context.Background(), input)
	if err != nil || len(plan.Definitions) != 1 {
		t.Fatalf("the object bucket planned %+v (%v)", plan.Definitions, diagnostics.Of(err))
	}
	return plan.Definitions[0], plan.Secrets
}

func plannedBucket(t *testing.T, catalog api.Catalog) ObjectBucket {
	t.Helper()
	block, _ := plannedBucketBlock(t, catalog)
	r, err := DecodeRequest(block.Request)
	if err != nil || r.ObjectBucket == nil {
		t.Fatalf("the bucket request %+v (%v)", r, err)
	}
	return *r.ObjectBucket
}

func withTLSRGW(service string) func(api.Value) api.Value {
	return func(spec api.Value) api.Value {
		spec = withPairAndRGW(spec)
		return spec.WithPath(api.ListValue(tlsBinding(service, "gateway-tls")), "ceph", "serviceBindings")
	}
}

func TestObjectBucketBlockRequiresItsOwner(t *testing.T) {
	block, _ := plannedBucketBlock(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data")))
	if block.ID != "ceph-object-bucket-app-data" || block.ID != ObjectBucketBlockID("app-data") || block.Kind != string(api.StorageObjectBucket) || block.Object != "app-data" ||
		block.Stage != reconciliation.StageClusters || !slices.Equal(block.Exclusive, []string{"ceph/example"}) || block.Description != "realize Ceph object bucket app-data" {
		t.Fatalf("the object bucket block is %+v", block)
	}
	if !slices.Equal(block.Dependencies, []string{"ceph-cluster-example", "ceph-image-example"}) ||
		!slices.Equal(block.Requires, []reconciliation.ObjectRef{{Kind: string(api.StorageObjectUser), Object: "app-writer"}}) {
		t.Fatalf("the object bucket block depends on %v and requires %v", block.Dependencies, block.Requires)
	}
	removal, err := New(api.StorageObjectBucket, nil).Removal(context.Background(), reconciliation.Block{BlockDefinition: block})
	if err != nil || removal.Description != "remove Ceph object bucket app-data" || !slices.Equal(removal.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("the object bucket removal is %+v (%v)", removal, err)
	}
}

func TestObjectBucketRequestGolden(t *testing.T) {
	block, secretRefs := plannedBucketBlock(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data", "policy", declaredPolicy())))
	matchesGolden(t, "request-object-bucket", block.Request, false)
	r, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	want := ObjectBucket{Backend: "rgw.objects", Gateway: "objects", Name: "app-data", Owner: "app-writer", Policy: bucketPolicyText, Port: 80, Realm: "objects", Zone: "objects", ZoneGroup: "objects"}
	if r.Action != "object-bucket" || r.ObjectBucket == nil || *r.ObjectBucket != want || r.Gateway != nil || r.Pool != nil || r.ObjectUser != nil || slices.Contains(secretRefs, "gateway-tls") {
		t.Fatalf("the object bucket froze as %+v %+v with secrets %v", r, r.ObjectBucket, secretRefs)
	}
	if bucket := plannedBucket(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data"))); bucket.Policy != "" {
		t.Fatalf("an omitted policy froze as %q", bucket.Policy)
	}
	port := func(spec api.Value) api.Value {
		spec = withCorePair(spec)
		service := requestMap("service_type", "rgw", "service_id", "objects", "placement", requestMap("label", "mon", "count", 2), "spec", requestMap("rgw_frontend_port", 8080))
		return spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), service)...), "ceph", "services")
	}
	if bucket := plannedBucket(t, bucketCatalog(port, objectGateway("objects"), objectBucketObject("app-data"))); bucket.Port != 8080 || bucket.CertificateRef != "" {
		t.Fatalf("an authored frontend port froze as %+v", bucket)
	}
}

func TestObjectBucketPinsATLSBackendToItsBoundCertificateOnly(t *testing.T) {
	gateway := objectGateway("objects", "endpoint", requestMap("fqdn", "s3.example.test"))
	block, secretRefs := plannedBucketBlock(t, bucketCatalog(withTLSRGW("rgw.objects"), gateway, objectBucketObject("app-data")))
	r, err := DecodeRequest(block.Request)
	if err != nil {
		t.Fatal(err)
	}
	if b := r.ObjectBucket; b.Port != 443 || b.Endpoint != "s3.example.test" || b.CertificateRef != "gateway-tls" || !slices.Contains(secretRefs, "gateway-tls") {
		t.Fatalf("a TLS backend froze as %+v with secrets %v", b, secretRefs)
	}
	r.Owner, r.FSID = "apply-original", FSIDFor("apply-original", r.Cluster)
	for _, operation := range []string{"apply", "observe", "destroy"} {
		files, err := gatewayMaterial(lifecycle.Execution{Material: map[string]secrets.Material{"gateway-tls": secrets.NewMaterial(map[secrets.Part][]byte{
			secrets.CertificatePart: []byte(gatewayCertificatePEM), secrets.PrivateKeyPart: []byte(gatewayKeyPEM)})}}, r, operation)
		want := []lifecycle.MaterialFile{{Name: "gateway-certificate", Secret: "gateway-tls", Part: secrets.CertificatePart, Variable: "gatewayCertificate"}}
		if err != nil || !reflect.DeepEqual(files, want) {
			t.Fatalf("%s lends the bucket %+v (%v)", operation, files, err)
		}
	}
	ingress := func(spec api.Value) api.Value {
		spec = withPairAndHTTPRGW(spec)
		spec = spec.WithPath(api.ListValue(append(spec.Get("ceph", "services").Items(), ingressService("rgw.objects"))...), "ceph", "services")
		return spec.WithPath(api.ListValue(tlsBinding("ingress.rgw.objects", "gateway-tls")), "ceph", "serviceBindings")
	}
	if b := plannedBucket(t, bucketCatalog(ingress, gateway, objectBucketObject("app-data"))); b.Port != 80 || b.Endpoint != "" || b.CertificateRef != "" {
		t.Fatalf("a bucket behind a TLS ingress froze as %+v", b)
	}
}

func TestObjectBucketPlanRefusals(t *testing.T) {
	long := strings.Repeat("b", 45)
	_, err := planned(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject(long)), api.StorageObjectBucket, long)
	refusedWith(t, err, "api.invariant", "StorageObjectBucket "+long+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageObjectBucket to at most 44 characters")
	if !reconciliation.ValidSegment(ObjectBucketBlockID(strings.Repeat("b", 44))) {
		t.Fatal("a 44-character bucket name does not fit its block identity")
	}
	float := requestMap("Version", "2012-10-17", "Statement", api.ListValue(requestMap("Condition", requestMap("NumericLessThan", requestMap("s3:max-keys", api.NumberValue("1.5"))))))
	_, err = planned(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data", "policy", float)), api.StorageObjectBucket, "app-data")
	refusedWith(t, err, "api.invariant", "StorageObjectBucket app-data declares a policy holding a value that is not a JSON string, integer, boolean, list or mapping",
		"write spec.policy as the native S3 bucket policy document with only strings, integers, booleans, lists and mappings")
	const typeMessage = "StorageObjectBucket app-data declares a policy holding a value that is not a JSON string, integer, boolean, list or mapping"
	const typeRemedy = "write spec.policy as the native S3 bucket policy document with only strings, integers, booleans, lists and mappings"
	for name, value := range map[string]api.Value{"a null": {}, "an integer beyond int64": api.IntegerValue("9223372036854775808"), "an exponent": api.NumberValue("1e3")} {
		bad := requestMap("Version", "2012-10-17", "Statement", api.ListValue(requestMap("Sid", value)))
		_, err = planned(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data", "policy", bad)), api.StorageObjectBucket, "app-data")
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Message != typeMessage || d[0].Remediation != typeRemedy {
			t.Fatalf("%s: %+v", name, d)
		}
	}
	large := requestMap("Version", "2012-10-17", "Statement", api.ListValue(requestMap("Sid", strings.Repeat("s", 65536))))
	_, err = planned(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data", "policy", large)), api.StorageObjectBucket, "app-data")
	refusedWith(t, err, "api.invariant", "StorageObjectBucket app-data declares a policy larger than 65536 bytes of JSON", "shorten spec.policy to at most 65536 bytes of JSON")
	catalog := planCatalog(3, 2, withPairAndHTTPRGW, objectGateway("objects"), objectUserObject("app-writer"), objectBucketObject("app-data"))
	_, gatewayErr := gatewayRequests(catalog, objectGateway("objects"), "synthetic")
	_, err = planned(t, catalog, api.StorageObjectBucket, "app-data")
	if gatewayErr == nil || err == nil || !reflect.DeepEqual(diagnostics.Of(err), diagnostics.Of(gatewayErr)) {
		t.Fatalf("the bucket refused %v; its gateway refused %v", diagnostics.Of(err), diagnostics.Of(gatewayErr))
	}
}

func TestObjectBucketDeadline(t *testing.T) {
	r := Request{Action: "object-bucket", ObjectBucket: &ObjectBucket{Name: "app-data"}}
	for operation, want := range map[string]time.Duration{"apply": 2280 * time.Second, "observe": 1380 * time.Second, "destroy": 1380 * time.Second} {
		if got, err := cephDeadline(r, operation); err != nil || got != want {
			t.Fatalf("%s deadline %v (%v), want %v", operation, got, err, want)
		}
	}
}

func bucketRuntime() Request {
	r := runtimeRequest()
	r.Action, r.ObjectBucket = "object-bucket", &ObjectBucket{Backend: "rgw.objects", Gateway: "objects", Name: "app-data", Owner: "app-writer", Port: 80, Realm: "objects", Zone: "objects", ZoneGroup: "objects"}
	return r
}

func TestObjectBucketEvidenceNamesOnlyItsBucket(t *testing.T) {
	r := bucketRuntime()
	ready := readyEvidence(r)
	ready.Request = "digest"
	ready.Resource = ResourceEvidence{ID: "app-data"}
	if effect, err := EvaluateEvidence(evidenceBytes(t, ready), r, "digest", false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready bucket evidence %v (%v)", effect, err)
	}
	partial := ready
	partial.Ready, partial.Partial, partial.Resource = false, true, ResourceEvidence{}
	if effect, err := EvaluateEvidence(evidenceBytes(t, partial), r, "digest", false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial bucket evidence %v (%v)", effect, err)
	}
	for name, change := range map[string]func(*Evidence){
		"no bucket name":   func(e *Evidence) { e.Resource = ResourceEvidence{} },
		"another bucket":   func(e *Evidence) { e.Resource.ID = "other" },
		"its owner":        func(e *Evidence) { e.Resource.ID = "app-writer" },
		"a path":           func(e *Evidence) { e.Resource.Path = "/app-data" },
		"a client":         func(e *Evidence) { e.Client = "client.app-writer" },
		"the user's shape": func(e *Evidence) { e.Action = "object-user" },
	} {
		e := ready
		change(&e)
		if _, err := EvaluateEvidence(evidenceBytes(t, e), r, "digest", false); err == nil {
			t.Errorf("bucket evidence with %s was accepted", name)
		}
	}
}

func TestObjectBucketScopeRowNamesItsOwnerAndZone(t *testing.T) {
	block := frozenScopeBlock(t, api.StorageObjectBucket, "app-data", bucketRuntime())
	if got := ResourceScope(api.Catalog{}, api.StorageObjectBucket, "app-data", []reconciliation.Block{block}); got != "bucket app-data of user app-writer in zone objects/objects/objects" {
		t.Fatalf("the object bucket scope row reads %q", got)
	}
}

func TestObjectBucketRunsWithoutOutputsOrProducedMaterial(t *testing.T) {
	frozen := coreRequest()
	frozen.Action = "object-bucket"
	frozen.ObjectBucket = &ObjectBucket{Backend: "rgw.objects", CertificateRef: "gateway-tls", Endpoint: "s3.example.test", Gateway: "objects", Name: "app-data", Owner: "app-writer",
		Policy: bucketPolicyText, Port: 443, Realm: "objects", Zone: "objects", ZoneGroup: "objects"}
	e := executionFor(t, frozen)
	e.Block.ID, e.Block.Kind, e.Block.Object = ObjectBucketBlockID("app-data"), string(api.StorageObjectBucket), "app-data"
	e = withMaterial(e, frozen)
	e.Material["gateway-tls"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte(gatewayCertificatePEM), secrets.PrivateKeyPart: []byte(gatewayKeyPEM)})
	runtime := frozen
	runtime.Owner, runtime.FSID = e.ApplyOperation, FSIDFor(e.ApplyOperation, frozen.Cluster)
	evidence := readyEvidence(runtime)
	evidence.Request, evidence.Resource = e.Block.RequestDigest, ResourceEvidence{ID: "app-data"}
	runner := &recordingRunner{result: lifecycle.RunResult{Evidence: evidenceBytes(t, evidence)}}
	observation, err := New(api.StorageObjectBucket, runner).Observe(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(observation.Produced) != 0 {
		t.Fatalf("the bucket observation returned %+v (%v)", observation, diagnostics.Of(err))
	}
	if len(runner.request.Outputs) != 0 || runner.request.Deadline != 1380*time.Second || !slices.ContainsFunc(runner.request.Materials, func(f lifecycle.MaterialFile) bool { return f.Variable == "gatewayCertificate" }) ||
		slices.ContainsFunc(runner.request.Materials, func(f lifecycle.MaterialFile) bool { return f.Variable == "gatewayKey" }) {
		t.Fatalf("the bucket observation ran %+v", runner.request)
	}
	runner.result.Produced = userProduced(userMaterial)
	if observation, err := New(api.StorageObjectBucket, runner).Observe(context.Background(), e); err == nil || observation.Effect != reconciliation.EffectUnknown {
		t.Fatalf("produced material from a bucket was accepted: %+v (%v)", observation, err)
	}
	mismatched := e
	mismatched.Block.Kind = string(api.StorageObjectUser)
	if _, err := New(api.StorageObjectBucket, runner).Observe(context.Background(), mismatched); err == nil {
		t.Fatal("a bucket request under another kind was accepted")
	}
}

func TestObjectBucketPolicyFreezesAsTheTextTheWorkerAdmits(t *testing.T) {
	statement := requestMap("Z", "z", "Sid", "<>& \u00e9\u2028\u2029\t\"\\\x01\x7f\b\f\n\r\u20ac", "N", api.IntegerValue("-9223372036854775808"), "K\u00e9", true)
	policy := requestMap("Version", "2012-10-17", "Statement", api.ListValue(statement))
	const want = "{\"Statement\":[{\"K\u00e9\":true,\"N\":-9223372036854775808,\"Sid\":\"\\u003c\\u003e\\u0026 \u00e9\\u2028\\u2029\\t\\\"\\\\\\u0001\x7f\\b\\f\\n\\r\u20ac\",\"Z\":\"z\"}],\"Version\":\"2012-10-17\"}"
	if got := plannedBucket(t, bucketCatalog(withPairAndHTTPRGW, objectGateway("objects"), objectBucketObject("app-data", "policy", policy))).Policy; got != want {
		t.Fatalf("the policy froze as %s, want %s", got, want)
	}
}
